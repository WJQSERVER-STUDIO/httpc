package httpc

import (
	"context"
	"net"
	"time"
)

// newCustomResolver 创建一个使用指定DNS服务器的net.Resolver实例
// servers: DNS服务器地址列表, 格式为 "ip:port", 例如 "8.8.8.8:53"
// timeout: DNS查询的超时时间
func newCustomResolver(servers []string, timeout time.Duration) *net.Resolver {
	// 如果未提供服务器地址, 则返回nil, 使用系统默认解析器
	if len(servers) == 0 {
		return nil
	}

	return &net.Resolver{
		// 设置为true以确保Go使用纯Go实现的DNS客户端, 这样我们的Dial函数才会被调用
		PreferGo: true,
		// 自定义Dial函数, 用于连接到指定的DNS服务器
		Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
			d := net.Dialer{
				Timeout: timeout,
			}
			// 从提供的服务器列表中选择一个进行连接 (此处简单使用第一个)
			// network参数通常是 "udp"或"tcp", 我们直接使用它
			// address参数会被忽略, 因为我们强制使用我们自己的服务器地址
			return d.DialContext(ctx, network, servers[0])
		},
	}
}
