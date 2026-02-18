package httpc

import (
	"strings"
	"bytes"
	"sync"
)

var bufferPool = sync.Pool{
	New: func() any {
		return bytes.NewBuffer(make([]byte, 0, defaultBufferSize))
	},
}

// BufferPool 缓冲池接口
type BufferPool interface {
	Get() *bytes.Buffer
	Put(*bytes.Buffer)
}

// 默认缓冲池实现
type defaultPool struct {
	bufferSize int
}

func newDefaultPool(bufferSize int) *defaultPool {
	return &defaultPool{bufferSize: bufferSize}
}

func (p *defaultPool) Get() *bytes.Buffer {
	buf := bufferPool.Get().(*bytes.Buffer)
	buf.Reset()
	return buf
}

func (p *defaultPool) Put(buf *bytes.Buffer) {
	if buf.Cap() > p.bufferSize*2 { // 防止内存泄漏，基于配置的 bufferSize
		return
	}
	bufferPool.Put(buf)
}

var stringsBuilderPool = sync.Pool{
	New: func() any {
		return &strings.Builder{}
	},
}
