package main

import (
	"flag"
	//"fmt"
	"log"
	. "natproxy/natproxy"
	"net"
	"os"
	"strconv"
	//"path/filepath"
	"runtime"
	//"time"
)

type Options struct {
	isServer    bool
	isClient    bool
	runPath     string
	cfgFilename string
	sn          string
	addr        string
	port        int
}

func work(opt *Options) {
	cfg := &Config{}
	if (opt.isServer && opt.isClient) || !(opt.isServer || opt.isClient) {
		Log.Println("Must run as Server mode(-s) or Client mode(-c).")
		goto mainend
	}

	if opt.isServer == true {
		if e := ParseCfgFile(cfg, opt.cfgFilename); e != nil {
			Log.Println("Parse Config File Error", e)
			return
		}
		Log.Println("Run as server proxy.")
		server := NewProxyServer(
			cfg.Server.IP+":"+strconv.Itoa(cfg.Server.Port),
			cfg.Server.IP+":"+strconv.Itoa(cfg.Server.Serv),
			"127.0.0.1:"+strconv.Itoa(cfg.Braport),
		)
		go server.Run()
	}

	if opt.isClient == true {
		if opt.sn != "" {
			cfg.Sn = opt.sn
		}
		Log.Println("Run as client proxy, SN", cfg.Sn)
		client := NewProxyClient(
			opt.addr+":"+strconv.Itoa(opt.port),
			net.TCPAddr{net.ParseIP("127.0.0.1"), 5900, ""},
			cfg.Sn,
		)
		go client.Run()
	}

	// wait for end
	runtime.Gosched()
mainend:
	Wg.Wait()
	Log.Println("Terminated")

}

func main() {
	var opt = new(Options)
	flag.BoolVar(&opt.isServer, "s", false, "Run as a server.")
	flag.BoolVar(&opt.isClient, "c", false, "Run as a client.")
	flag.StringVar(&opt.runPath, "r", "", "Just ignore it.")
	flag.StringVar(&opt.cfgFilename, "f", "config.json", "Config File Name.")
	flag.StringVar(&opt.sn, "i", "", "Serial No.")
	flag.IntVar(&opt.port, "p", 0, "Port")
	flag.StringVar(&opt.addr, "a", "", "Address")
	flag.Parse()

	// first run, not daemon mode, fork myself as daemon program
	// if opt.runPath == "" {
	// 	filePath, _ := filepath.Abs(os.Args[0])
	// 	args := append([]string{filePath}, os.Args[1:]...)
	// 	args = append(args, "-r="+filepath.Dir(filePath))
	// 	fmt.Println(args)
	// 	os.StartProcess(filePath, args, &os.ProcAttr{Files: []*os.File{os.Stdin, os.Stdout, os.Stderr}})
	// 	return
	// }
	// // second run, daemon mode
	// tmNow := []byte(time.Now().Format("2006-01-02 15:04:05"))
	// for p, v := range tmNow {
	// 	if v == ' ' || v == ':' {
	// 		tmNow[p] = '-'
	// 	}
	// }
	// filename := opt.runPath + "/logfile-" + string(tmNow)
	// if logfile, e := os.OpenFile(filename, os.O_CREATE|os.O_RDWR|os.O_TRUNC, 0666); e == nil {
	// 	Log = log.New(logfile, "", log.LstdFlags)
	// } else {
	// 	fmt.Println(e)
	// 	Log = log.New(os.Stderr, "", log.LstdFlags)
	// }

	Log = log.New(os.Stderr, "", log.LstdFlags)
	work(opt)
}
