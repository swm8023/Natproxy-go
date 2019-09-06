package natproxy

import (
    "encoding/json"
    "io/ioutil"
    "log"
    "net"
    "strconv"
    "sync"
)

type ServerCfg struct {
    IP   string `json:"ip"`
    Port int    `json:"port"`
    Serv int    `json:"serv"`   
}

type MappingCfg struct {
    IP   string `json:"ip"`
    Port int    `json:"port"`
}

type Config struct {
    Sn      string       `json:"sn"`
    Server  ServerCfg    `json:"server"`
    Braport int          `json:"braport"`
    Mapping []MappingCfg `json:"mapping"`
}

var Log *log.Logger
var Wg sync.WaitGroup

func ParseCfgFile(cfg *Config, fname string) error {
    data, e := ioutil.ReadFile(fname)
    if e != nil {
        return e
    }
    if e := json.Unmarshal(data, &cfg); e != nil {
        return e    
    }
    return nil
}

var idGener = make(chan uint32)

func init() {
    go func() {
        for i := uint32(0); ; i++ {
            idGener <- i
        }
    }()
}

func GetUniqId() uint32 {
    return <-idGener
}

func toAddrStr(ip net.IP, port int) string {
    return ip.String() + ":" + strconv.Itoa(port)
}
