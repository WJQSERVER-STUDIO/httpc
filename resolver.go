package httpc

import (
	"context"
	"fmt"
	"net"
	"time"
)

// customDialer 包装了标准的 net.Dialer, 以实现一个支持轮询和回退的自定义DNS解析流程
// 它将被用于替换 http.Transport 中默认的 DialContext 方法
type customDialer struct {
	defaultDialer *net.Dialer   // 用于建立TCP/UDP连接, 并在自定义DNS失败时作为回退选项
	dnsServers    []string      // 自定义DNS服务器地址列表 (格式 "ip:port")
	dnsTimeout    time.Duration // 单次DNS查询的超时时间
}

// DialContext 是实现核心逻辑的地方它拦截了所有的拨号请求
// 流程: 尝试用自定义DNS解析 -> 如果成功, 则连接到解析出的IP -> 如果失败, 则回退到默认拨号器处理
func (d *customDialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	// 1. 从地址中分离出 host 和 port (例如, 从 "example.com:443" 中提取 "example.com")
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		// 如果分离失败 (例如, 地址格式不标准), 直接回退到默认拨号器, 保证兼容性
		return d.defaultDialer.DialContext(ctx, network, address)
	}

	// 2. 尝试使用自定义DNS服务器列表来解析域名
	ips, resolveErr := d.resolveWithCustomDNS(ctx, host)

	// 3. 处理解析结果
	if resolveErr != nil {
		// 回退: 使用原始的 dialer 和 address, 让系统处理DNS解析和连接
		return d.defaultDialer.DialContext(ctx, network, address)
	}

	// 如果自定义解析成功, `ips` 列表中会有一个或多个IP地址
	// 4. 尝试连接到所有解析出的IP地址, 直到成功为止
	var firstDialErr error
	for _, ip := range ips {
		// 将解析出的IP和原始端口组合成新的拨号地址
		dialAddr := net.JoinHostPort(ip.String(), port)

		// 使用默认拨号器连接到这个具体的IP地址
		conn, dialErr := d.defaultDialer.DialContext(ctx, network, dialAddr)
		if dialErr == nil {
			// 连接成功, 立即返回
			return conn, nil
		}

		// 如果连接失败, 保存第一个遇到的错误, 以便在所有尝试都失败后返回
		if firstDialErr == nil {
			firstDialErr = dialErr
		}
	}

	// 5. 如果循环结束仍未成功连接, 返回保存的第一个错误
	if firstDialErr == nil {
		// 这种情况很罕见, 意味着解析成功但返回了一个空的IP列表
		return nil, fmt.Errorf("httpc: custom DNS resolved host %s but no IP addresses were found", host)
	}

	return nil, firstDialErr
}

// resolveWithCustomDNS 使用自定义的DNS服务器列表来解析主机名
// 它会按顺序尝试列表中的每个DNS服务器, 直到有一个成功返回结果
func (d *customDialer) resolveWithCustomDNS(ctx context.Context, host string) ([]net.IP, error) {
	// 创建一个临时的 net.Resolver 实例, 其拨号逻辑被我们重写
	resolver := &net.Resolver{
		// 必须设置为 true, Go才会使用我们自定义的 Dial 函数
		PreferGo: true,
		// 自定义拨号函数, 用于连接到DNS服务器本身
		Dial: func(dialCtx context.Context, network, address string) (net.Conn, error) {
			// 这个内部拨号器仅用于连接DNS服务器, 使用我们配置的超时时间
			dnsDialer := net.Dialer{Timeout: d.dnsTimeout}

			var lastErr error
			// 遍历所有提供的DNS服务器地址
			for _, server := range d.dnsServers {
				// 尝试连接到DNS服务器
				conn, err := dnsDialer.DialContext(dialCtx, network, server)
				if err == nil {
					// 连接成功, 返回连接供 Resolver 使用
					return conn, nil
				}
				lastErr = err // 保存错误, 继续尝试下一个
			}

			// 如果所有DNS服务器都连接失败, 返回最后一个遇到的错误
			return nil, fmt.Errorf("all custom DNS servers failed to connect: %w", lastErr)
		},
	}

	// 使用配置好的解析器执行域名查找
	return resolver.LookupIP(ctx, "ip", host)
}
