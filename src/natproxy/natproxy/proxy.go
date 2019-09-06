package natproxy

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"strconv"
	"time"
)

type Proxy struct {
	saddr string
	haddr string
	caddr net.TCPAddr
	sn    string
	side  int
	baddr string
}

type ProxyServer Proxy

type ProxyClient Proxy

func NewProxyServer(saddr, haddr, baddr string) *ProxyServer {
	return &ProxyServer{saddr: saddr, haddr: haddr, side: serverSide, baddr: baddr}
}

func NewProxyClient(saddr string, caddr net.TCPAddr, sn string) *ProxyClient {
	return &ProxyClient{saddr: saddr, caddr: caddr, sn: sn, side: clientSide}
}

func (server *ProxyServer) Run() {
	Wg.Add(1)
	defer Wg.Done()

	ec := make(chan error, 2)
	if l, e := net.Listen("tcp", server.saddr); e == nil {
		Log.Println("Server Listen at:", server.saddr)
		defer l.Close()
		go AcceptAndHandle(l, server.openTunnel, ec)
	} else {
		Log.Println("Server Listen at", server.haddr, "failed:", e)
		return
	}

	go func(ec chan error) {
		if e := http.ListenAndServe(server.haddr, server); e != nil {
			Log.Println("Http service run failed", e)
			ec <- e
		}
	}(ec)
	Log.Println("Http service run:", server.haddr)

	/* stopped */
	Log.Println("Server Stop", <-ec)
}

/* connection from client proxy */
func (server *ProxyServer) openTunnel(conn net.Conn) {
	defer conn.Close()

	Log.Println("accept proxy client from", conn.RemoteAddr().String())
	tun := &Tunnel{
		tid:   GetUniqId(),
		tconn: conn,
		rer:   bufio.NewReader(conn),
		//proxy:  server,
		side:   serverSide,
		status: staCreate,
		conns:  make(map[uint32]net.Conn),
		mport:  0,
		key:    "",
		baddr: server.baddr,
	}
	defer tun.close()

	tun.work()
}

func (client *ProxyClient) Run() {
	Wg.Add(1)
	defer Wg.Done()

	for {
		if conn, e := net.Dial("tcp", client.saddr); e == nil {
			client.openTunnel(conn)
		} else {
			Log.Println("open connection to ", client.saddr, "failed:", e)
		}
		time.Sleep(time.Second * 60)
	}
}

func (client *ProxyClient) openTunnel(conn net.Conn) {
	defer conn.Close()

	Log.Println("open connetion to", conn.RemoteAddr().String())
	tun := &Tunnel{
		tid:    0,
		tconn:  conn,
		rer:    bufio.NewReader(conn),
		csn:    client.sn,
		cip:    client.caddr.IP,
		cport:  client.caddr.Port,
		side:   clientSide,
		status: staCreate,
		conns:  make(map[uint32]net.Conn),
	}
	defer tun.close()

	tun.initPeer()
	tun.work()
}

func AcceptAndHandle(l net.Listener, handle func(net.Conn), ec chan error) {
	for {
		if c, e := l.Accept(); e != nil {
			Log.Println(e)
		} else {
			go handle(c)
		}
	}
	ec <- errors.New("Accept Stopped")
}

/* HTTP */
type tunrec struct {
	Sn    string
	Ip    string
	Port  int
	Mport int
}

type respStartport struct {
	Errcode int `json:"errcode"`
	Mport   int `json:"mport"`
}

type tunrecs []tunrec

func (proxy *ProxyServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	jstr := []byte{}
	switch r.URL.Path {
	case "/list":
	case "/startserv":
		sn := r.Form["sn"][0]
		ip := r.Form["ip"][0]
		sv := r.Form["service"][0]
		pt := 0
		switch sv {
		case "vnc":
			pt = 5900
		default:
		}
		if p, e := proxy.startPort(sn, ip, pt); e == nil {
			jstr, _ = json.Marshal(respStartport{Errcode: 0, Mport: p})
			break
		}
		jstr, _ = json.Marshal(respStartport{Errcode: -1, Mport: 0})
	case "/stopserv":
	default:
		http.NotFound(w, r)
	}
	w.Header().Set("Content-Type", "application/json")
	if len(r.Form["jsonp"]) > 0 {
		io.WriteString(w, "jsonpHandler("+string(jstr[:])+")")
	} else {
		io.WriteString(w, string(jstr[:]))
	}
}

func (proxy *ProxyServer) startPort(sn, ip string, port int) (int, error) {
	tunlock.Lock()
	defer tunlock.Unlock()

	key := "<" + sn + ":" + ip + ":" + strconv.Itoa(port) + ">"
	if tun, ok := tunnels[key]; ok == false {
		return -1, errors.New("not exist")
	} else {
		if tun.status == staInit {
			tun.startListen()
		}
		return tun.mport, nil
	}
	return 0, nil
}
