package proxy

import (
	"errors"
	"net"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"

	"ehang.io/nps/bridge"
	"ehang.io/nps/lib/common"
	"ehang.io/nps/lib/conn"
	"ehang.io/nps/lib/file"
	"ehang.io/nps/server/connection"
	"github.com/astaxie/beego"
	"github.com/astaxie/beego/logs"
)

type TunnelModeServer struct {
	BaseServer
	process  process
	listener net.Listener
}

//tcp|http|host
func NewTunnelModeServer(process process, bridge NetBridge, task *file.Tunnel) *TunnelModeServer {
	s := new(TunnelModeServer)
	s.bridge = bridge
	s.process = process
	s.task = task
	return s
}

//开始
func (s *TunnelModeServer) Start() error {
	if err := s.Listen(); err != nil {
		return err
	}
	s.Serve()
	return nil
}

func (s *TunnelModeServer) Listen() error {
	host := s.task.ServerIp
	if strings.HasPrefix(host, "[") && strings.HasSuffix(host, "]") {
		host = host[1 : len(host)-1]
	}
	var err error
	s.listener, err = net.Listen("tcp", net.JoinHostPort(host, strconv.Itoa(s.task.Port)))
	return err
}

func (s *TunnelModeServer) Serve() {
	conn.Accept(s.listener, func(c net.Conn) {
		if err := s.CheckFlowAndConnNum(s.task.Client); err != nil {
			logs.Warn("client id %d, task id %d,error %s, when tcp connection", s.task.Client.Id, s.task.Id, err.Error())
			c.Close()
			return
		}
		defer s.task.Client.AddConn()
		if s.task.Mode == "tcp" {
			if ip, _, err := net.SplitHostPort(c.RemoteAddr().String()); err == nil {
				s.task.RecordSourceIP(ip)
			}
		}
		logs.Trace("new tcp connection,local port %d,client %d,remote address %s", s.task.Port, s.task.Client.Id, c.RemoteAddr())
		s.process(conn.NewConn(c), s)
	})
}

//close
func (s *TunnelModeServer) Close() error {
	if s.listener == nil {
		return nil
	}
	return s.listener.Close()
}

//web管理方式
type WebServer struct {
	BaseServer
}

//开始
func (s *WebServer) Start() error {
	p, _ := beego.AppConfig.Int("web_port")
	if p == 0 {
		stop := make(chan struct{})
		<-stop
	}
	beego.BConfig.WebConfig.Session.SessionOn = true
	beego.SetStaticPath(beego.AppConfig.String("web_base_url")+"/static", filepath.Join(common.GetRunPath(), "web", "static"))
	beego.SetViewsPath(filepath.Join(common.GetRunPath(), "web", "views"))
	err := errors.New("Web management startup failure ")
	var l net.Listener
	if l, err = connection.GetWebManagerListener(); err == nil {
		beego.InitBeforeHTTPRun()
		if beego.AppConfig.String("web_open_ssl") == "true" {
			keyPath := beego.AppConfig.String("web_key_file")
			certPath := beego.AppConfig.String("web_cert_file")
			err = http.ServeTLS(l, beego.BeeApp.Handlers, certPath, keyPath)
		} else {
			err = http.Serve(l, beego.BeeApp.Handlers)
		}
	} else {
		logs.Error(err)
	}
	return err
}

func (s *WebServer) Close() error {
	return nil
}

//new
func NewWebServer(bridge *bridge.Bridge) *WebServer {
	s := new(WebServer)
	s.bridge = bridge
	return s
}

type process func(c *conn.Conn, s *TunnelModeServer) error

//tcp proxy
func ProcessTunnel(c *conn.Conn, s *TunnelModeServer) error {
	s.task.Lock()
	targetAddr, err := s.task.Target.GetRandomTarget()
	s.task.Unlock()
	if err != nil {
		c.Close()
		logs.Warn("tcp port %d ,client id %d,task id %d connect error %s", s.task.Port, s.task.Client.Id, s.task.Id, err.Error())
		return err
	}
	return s.DealClient(c, s.task.Client, targetAddr, nil, common.CONN_TCP, nil, s.task.Flow, s.task.Target.LocalProxy)
}

//http proxy
func ProcessHttp(c *conn.Conn, s *TunnelModeServer) error {
	_, addr, rb, err, r := c.GetHost()
	if err != nil {
		c.Close()
		logs.Info(err)
		return err
	}
	if r.Method == "CONNECT" {
		c.Write([]byte("HTTP/1.1 200 Connection established\r\n\r\n"))
		rb = nil
	}
	if err := s.auth(r, c, s.task.Client.Cnf.U, s.task.Client.Cnf.P); err != nil {
		return err
	}
	return s.DealClient(c, s.task.Client, addr, rb, common.CONN_TCP, nil, s.task.Flow, s.task.Target.LocalProxy)
}
