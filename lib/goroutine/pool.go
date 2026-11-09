package goroutine

import (
	"ehang.io/nps/lib/common"
	"ehang.io/nps/lib/file"
	"github.com/panjf2000/ants/v2"
	"io"
	"net"
	"sync"
)

type connGroup struct {
	src      io.ReadWriteCloser
	dst      io.ReadWriteCloser
	wg       *sync.WaitGroup
	flow     *file.Flow
	incoming bool
}

type flowWriter struct {
	io.Writer
	flow     *file.Flow
	incoming bool
}

func (w flowWriter) Write(p []byte) (int, error) {
	n, err := w.Writer.Write(p)
	if n > 0 {
		if w.incoming {
			w.flow.Add(int64(n), 0)
		} else {
			w.flow.Add(0, int64(n))
		}
	}
	return n, err
}

func copyConnGroup(group interface{}) {
	cg, ok := group.(connGroup)
	if !ok {
		return
	}
	var dst io.Writer = cg.dst
	if cg.flow != nil {
		dst = flowWriter{cg.dst, cg.flow, cg.incoming}
	}
	_, err := common.CopyBuffer(dst, cg.src)
	if err != nil {
		cg.src.Close()
		cg.dst.Close()
		//logs.Warn("close npc by copy from nps", err, c.connId)
	}
	cg.wg.Done()
}

type Conns struct {
	conn1 io.ReadWriteCloser // mux connection
	conn2 net.Conn           // outside connection
	flow  *file.Flow
	wg    *sync.WaitGroup
}

func NewConns(c1 io.ReadWriteCloser, c2 net.Conn, flow *file.Flow, wg *sync.WaitGroup) Conns {
	return Conns{
		conn1: c1,
		conn2: c2,
		flow:  flow,
		wg:    wg,
	}
}

func copyConns(group interface{}) {
	conns := group.(Conns)
	wg := new(sync.WaitGroup)
	wg.Add(2)
	_ = connCopyPool.Invoke(connGroup{src: conns.conn2, dst: conns.conn1, wg: wg, flow: conns.flow, incoming: true})
	_ = connCopyPool.Invoke(connGroup{src: conns.conn1, dst: conns.conn2, wg: wg, flow: conns.flow})
	wg.Wait()
	conns.wg.Done()
}

var connCopyPool, _ = ants.NewPoolWithFunc(200000, copyConnGroup, ants.WithNonblocking(false))
var CopyConnsPool, _ = ants.NewPoolWithFunc(100000, copyConns, ants.WithNonblocking(false))
