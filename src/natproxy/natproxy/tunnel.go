package natproxy

import (
	"encoding/binary"
	"errors"
	"io"
	"math/rand"
	"net"
	"strconv"
	"sync"
	"time"
)

const (
	opInit  = 0
	opOpen  = 1
	opData  = 2
	opClose = 3
	opGetid = 4
	opKeep  = 5
	opStart = 6
	opStop  = 7

	serverSide = 0
	clientSide = 1

	staCreate  = 0
	staInit    = 1
	staMapping = 2
)

type Tunnel struct {
	tid    uint32
	tconn  net.Conn
	rer    io.Reader
	side   int
	csn    string
	cip    net.IP
	cport  int
	mport  int
	status int
	act    time.Time
	clock  sync.Mutex
	conns  map[uint32]net.Conn
	llock  sync.Mutex
	lis    net.Listener
	key    string
	baddr  string
}

var tunlock sync.Mutex
var tunnels map[string]*Tunnel

func init() {
	tunnels = make(map[string]*Tunnel)
}

func (tun *Tunnel) handleInitPack(id uint32, data []byte) error {
	// server proxy

	if len(data) < 6 {
		return errors.New("Package too short")
	}
	ip, port, sn := net.IP(data[:4]), int(binary.BigEndian.Uint16(data[4:6])), data[6:]
	tun.cip, tun.cport, tun.csn = ip, port, string(sn[:])
	tun.key = "<" + string(sn[:len(sn)]) + ":" + ip.String() + ":" + strconv.Itoa(port) + ">"
	Log.Println("Init Tunnel:", tun.key)
	tun.status = staInit

	tunlock.Lock()
	defer tunlock.Unlock()
	if v, es := tunnels[tun.key]; es && v != tun {
		return errors.New("Tunnel already exists.")
	}
	tunnels[tun.key] = tun

	return nil
}

func (tun *Tunnel) handleOpenPack(id uint32, data []byte) error {
	// client proxy
	conn, e := net.DialTimeout("tcp", tun.cip.String()+":"+strconv.Itoa(tun.cport), 10*time.Second)
	if e != nil {
		// connect error, tell peer close
		tun.sendPackageH(opClose, id)
		Log.Println("Tunnel Open Connection Error", e)
		return nil
	}
	tun.clock.Lock()
	tun.conns[id] = conn
	tun.clock.Unlock()
	go tun.servConnection(conn, id)

	return nil
}

func (tun *Tunnel) handleDataPack(id uint32, data []byte) error {
	// both client and server proxy
	tun.clock.Lock()
	tun.clock.Unlock()

	conn, es := tun.conns[id]
	if es == false {
		Log.Println("Connection not exsits When handle data package.")
		return nil
	}
	tun.act = time.Now()
	if _, e := conn.Write(data); e != nil {
		// write error, remove conn and tell peer close
		tun.sendPackageH(opClose, id)
		conn.Close()
		delete(tun.conns, id)
		return nil
	}
	return nil
}

func (tun *Tunnel) handleClosePack(id uint32, data []byte) error {
	// both client and server proxy
	tun.clock.Lock()
	defer tun.clock.Unlock()

	conn, es := tun.conns[id]
	if es == false {
		return nil
	}
	conn.Close()
	delete(tun.conns, id)

	return nil
}

func (tun *Tunnel) handleGetidPack(id uint32, data []byte) error {
	switch tun.side {
	case serverSide:
		tun.sendPackageH(opGetid, tun.tid)
	case clientSide:
		tun.tid = id
	}
	return nil
}

func (tun *Tunnel) handleStartPack(id uint32, data []byte) error {
	switch tun.side {
	case serverSide:
		tun.startListen()
	case clientSide:
		tun.mport = int(id)
		Log.Println("Mapping", tun.mport, "=>", tun.cip.String()+":"+strconv.Itoa(tun.cport))
	}
	return nil
}

func (tun *Tunnel) handleStopPack(id uint32, data []byte) error {
	switch tun.side {
	case serverSide:
		tun.stopListen()
	case clientSide:
		Log.Println("Mapping", tun.mport, "End")
		tun.mport = 0

	}
	return nil
}

func (tun *Tunnel) startListen() {
	tun.llock.Lock()
	defer tun.llock.Unlock()

	if tun.status != staInit {
		return
	}
	p, l, e := tun.listenRand()
	if e == nil {
		tun.status, tun.lis, tun.mport = staMapping, l, p
		tun.sendPackageH(opStart, uint32(p))
		go func() {
			for tun.status == staMapping {
				if c, e := tun.lis.Accept(); e == nil {
					go tun.newConnection(c)
				}
			}
			Log.Println("Mapping", tun.lis.Addr().String(), "End")
		}()
		/* check function */
		go func() {
			tun.act = time.Now()
			for {
				time.Sleep(time.Second * 60)
				tun.clock.Lock()
				actconn := len(tun.conns)
				tun.clock.Unlock()
				if tun.act.Add(time.Second*60).Before(time.Now()) && actconn == 0 {
					tun.stopListen()
				}
			}
		}()
	}

}

func (tun *Tunnel) stopListen() {
	tun.llock.Lock()
	defer tun.llock.Unlock()

	if tun.status == staMapping {
		tun.sendPackageH(opStop, 0)
		tun.lis.Close()
		tun.status = staInit
		tun.mport = 0
		tun.tellBraServer();
	}
}

func (tun *Tunnel) initPeer() {
	// init first
	sbuf := []byte{}
	sbuf = append(sbuf, tun.cip.To4()...)
	sbuf = append(sbuf, []byte{byte(tun.cport >> 8), byte(tun.cport & 0xFF)}...)
	sbuf = append(sbuf, tun.csn[:]...)
	tun.sendPackage(opInit, 0, sbuf)

	// get tunnel id
	tun.sendPackageH(opGetid, 0)
}

func (tun *Tunnel) newConnection(conn net.Conn) {
	id := GetUniqId()
	tun.clock.Lock()
	tun.conns[id] = conn
	tun.clock.Unlock()
	tun.sendPackageH(opOpen, id)
	tun.servConnection(conn, id)
}

func (tun *Tunnel) servConnection(conn net.Conn, id uint32) {
	defer conn.Close()
	buf := make([]byte, 4096-9)
	for {
		if n, e := conn.Read(buf); e == nil {
			tun.act = time.Now()
			tun.sendPackage(opData, id, buf[:n])
		} else {
			break
		}
	}
	tun.clock.Lock()
	if _, e := tun.conns[id]; e == true {
		delete(tun.conns, id)
		tun.sendPackageH(opClose, id)
	}
	tun.clock.Unlock()
}

func (tun *Tunnel) listenRand() (int, net.Listener, error) {
	for p := rand.Intn(30) + 8050; p < 10000; p++ {
		if l, e := net.Listen("tcp", "0.0.0.0:"+strconv.Itoa(p)); e == nil {
			Log.Println("Mapping", l.Addr().String(), "=>", tun.cip.String()+":"+strconv.Itoa(tun.cport))
			return p, l, nil
		} else {
			continue
		}
	}
	return -1, nil, errors.New("Listen failed")
}

func (tun *Tunnel) sendPackageH(op byte, id uint32) error {
	return tun.sendPackage(op, id, []byte{})
}

func (tun *Tunnel) sendPackage(op byte, id uint32, data []byte) error {
	buf := []byte{0, 0, 0, 0, op, 0, 0, 0, 0}
	binary.BigEndian.PutUint32(buf, uint32(len(data)+9))
	binary.BigEndian.PutUint32(buf[5:], id)
	buf = append(buf, data...)
	//Log.Println("send:", buf)
	if e := tun.writeBuf(buf); e != nil {
		return e
	}
	return nil
}

func (tun *Tunnel) recvPackage(buf []byte) (op byte, id uint32, data []byte, err error) {
	if _, err = io.ReadAtLeast(tun.rer, buf[:4], 4); err != nil {
		return
	}
	pl := int(binary.BigEndian.Uint32(buf[:4]))
	if pl > len(buf) {
		err = errors.New("Package too long")
		return
	}
	if pl < 9 {
		err = errors.New("Package too short")
		return
	}
	if _, err = io.ReadAtLeast(tun.rer, buf[4:pl], pl-4); err != nil {
		return
	}
	op, id, data = buf[4], binary.BigEndian.Uint32(buf[5:9]), buf[9:pl]
	return
}

func (tun *Tunnel) writeBuf(buf []byte) error {
	for {
		if n, e := tun.tconn.Write(buf); e != nil {
			return e
		} else if n != len(buf) {
			buf = buf[n:]
		} else {
			break
		}
	}
	return nil
}

func (tun *Tunnel) work() {
	go tun.breath()
	buf := make([]byte, 4096)
	for {
		tun.tconn.SetReadDeadline(time.Now().Add(30 * time.Second))
		op, id, data, e := tun.recvPackage(buf)
		if e != nil {
			Log.Println(e)
			return
		}
		//Log.Println("recv:", op, id, data)
		switch op {
		// the first package after tunnel create
		case opInit:
			e = tun.handleInitPack(id, data)
		// data on mapping connection
		case opOpen:
			e = tun.handleOpenPack(id, data)
		case opData:
			e = tun.handleDataPack(id, data)
		// close a mapping connection
		case opClose:
			e = tun.handleClosePack(id, data)
		// get tunnel Id
		case opGetid:
			e = tun.handleGetidPack(id, data)
		case opStart:
			e = tun.handleStartPack(id, data)
		case opStop:
			e = tun.handleStopPack(id, data)
		case opKeep:
		default:
			e = errors.New("Error Operation")
		}
		if e != nil {
			Log.Println(e)
			break
		}
	}
}

func (tun *Tunnel) breath() {
	for {
		if e := tun.sendPackageH(opKeep, 0); e != nil {
			break
		}
		time.Sleep(time.Second * 10)
	}
}

func (tun *Tunnel) close() {
	tunlock.Lock()
	defer tunlock.Unlock()
	if tun.key != "" {
		delete(tunnels, tun.key)
	}
	tun.stopListen()
}


func (tun *Tunnel) tellBraServer() {
	// format end package
	Log.Println("here")
	buf := []byte{0,0,0,0,'W',0,0x10,0,0,0,0}
	binary.LittleEndian.PutUint32(buf, uint32(47))
	buf = append(buf, []byte{0x00, 0x20}...)
	buf = append(buf, tun.csn...);
	buf = append(buf, []byte{0x01, 0x05, 'p', 'r', 'o', 'x', 'y', 0x02, 0x01, 0x07, 0x03, 0x01, 0x00, 0xFE}...)
	
	Log.Println(buf)

	if conn, e := net.Dial("tcp", tun.baddr); e == nil {
		defer conn.Close();	

		conn.Write(buf);
	} else {
		Log.Println("send braserver failed!");
	}

}
