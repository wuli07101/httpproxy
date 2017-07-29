package main

import (
	"bytes"
	"net/textproto"
	"bufio"
	"fmt"
	"github.com/pkg/errors"
	kcp "github.com/xtaci/kcp-go"
	"github.com/xtaci/smux"
	"io"
	"log"
	"net/http"
	// "net/http/httputil"
	cmap "github.com/streamrail/concurrent-map"
	"net/http/httputil"
	"strings"
	"time"
)

var (
	kcpConfig Config
	objects   = cmap.New()
	httpid    int
)

type httpMuxesObj struct {
	session *smux.Session
	ttl     time.Time
}

type kcpObject struct {
	name  string
	muxes []httpMuxesObj
}

func gen_next_http_id() int {
	httpid += 1
	return httpid
}

func initKcpConn(name string) {
	config := kcpConfig
	numconn := uint16(config.Conn)

	muxes := make([]httpMuxesObj, numconn)

	for k := range muxes {
		muxes[k].session = waitKcpConn()
		muxes[k].ttl = time.Now().Add(time.Duration(config.AutoExpire) * time.Second)
	}

	obj := &kcpObject{
		name:  name,
		muxes: muxes,
	}

	addObject(obj)
}

func createKcpConn() (*smux.Session, error) {
	config := kcpConfig

	kcpconn, err := kcp.DialWithOptions(config.RemoteAddr, config.KcpBlock, config.DataShard, config.ParityShard)
	if err != nil {
		return nil, errors.Wrap(err, "createConn()")
	}
	kcpconn.SetStreamMode(true)
	kcpconn.SetWriteDelay(true)
	kcpconn.SetNoDelay(config.NoDelay, config.Interval, config.Resend, config.NoCongestion)
	kcpconn.SetWindowSize(config.SndWnd, config.RcvWnd)
	kcpconn.SetMtu(config.MTU)
	kcpconn.SetACKNoDelay(config.AckNodelay)

	if err := kcpconn.SetDSCP(config.DSCP); err != nil {
		log.Println("SetDSCP:", err)
	}
	if err := kcpconn.SetReadBuffer(config.SockBuf); err != nil {
		log.Println("SetReadBuffer:", err)
	}
	if err := kcpconn.SetWriteBuffer(config.SockBuf); err != nil {
		log.Println("SetWriteBuffer:", err)
	}

	smuxConfig := smux.DefaultConfig()
	smuxConfig.MaxReceiveBuffer = config.SockBuf
	smuxConfig.KeepAliveInterval = time.Duration(config.KeepAlive) * time.Second

	// stream multiplex
	var session *smux.Session
	if config.NoComp {
		session, err = smux.Client(kcpconn, smuxConfig)
	} else {
		session, err = smux.Client(NewCompStream(kcpconn), smuxConfig)
	}
	if err != nil {
		return nil, errors.Wrap(err, "createConn()")
	}
	log.Println("connection:", kcpconn.LocalAddr(), "->", kcpconn.RemoteAddr())
	return session, nil
}

// wait until a connection is ready
func waitKcpConn() *smux.Session {
	for {
		if session, err := createKcpConn(); err == nil {
			return session
		} else {
			log.Println("re-connecting:", err)
			time.Sleep(time.Second)
		}
	}
}

func handleHttpProxy_1(client io.ReadWriteCloser) {
	defer client.Close()

	bf := bufio.NewReader(client)
	req, err := http.ReadRequest(bf)
	if err != nil {
		log.Println(err)
		return
	}

	var address string
	if strings.Index(req.URL.Host, ":") == -1 {
		address = req.URL.Host + ":80"
	} else {
		address = req.URL.Host
	}
	fmt.Println(address)

	httpType := req.Header.Get("type")
	if httpTypeObject, result := findObject(httpType); result == true {
		config := kcpConfig
		httpid := gen_next_http_id()
		idx := uint16(httpid) % uint16(config.Conn)

		if httpTypeObject.muxes[idx].session.IsClosed() || (config.AutoExpire > 0 && time.Now().After(httpTypeObject.muxes[idx].ttl)) {
			httpTypeObject.muxes[idx].session = waitKcpConn()
			httpTypeObject.muxes[idx].ttl = time.Now().Add(time.Duration(config.AutoExpire) * time.Second)
		}
		smuxSession := httpTypeObject.muxes[idx].session

		smuxStream, err := smuxSession.OpenStream()
		if err != nil {
			return
		}
		defer smuxStream.Close()

		if req.Method == "CONNECT" {
			client.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n"))
		} else {
			dump, _ := httputil.DumpRequestOut(req, true)
			smuxStream.Write(dump)
			log.Printf("%s", dump)
		}

		// start tunnel
		p1die := make(chan struct{})
		go func() { io.Copy(client, smuxStream); close(p1die) }()

		p2die := make(chan struct{})
		go func() { io.Copy(smuxStream, client); close(p2die) }()

		// wait for tunnel termination
		select {
		case <-p1die:
		case <-p2die:
		}
	}
}

func handleHttpProxy(client io.ReadWriteCloser) {
	defer client.Close()

	bf := bufio.NewReader(client)
	tp := textproto.NewReader(bf)
	
	httpP,_ := tp.ReadLineBytes()

	mimeHeader, _ := tp.ReadMIMEHeader()
	reqHeader := http.Header(mimeHeader)

	var b bytes.Buffer
	b.Write(httpP)
	b.WriteString("\r\n")
	
   	var reqWriteExcludeHeaderDump = map[string]bool{}
	reqHeader.WriteSubset(&b, reqWriteExcludeHeaderDump)
	b.WriteString("\r\n")
    lastInt :=  bf.Buffered()
	if lastInt > 0 {
		lastBuf := make([]byte, lastInt)
	    bf.Read(lastBuf)
		b.Write(lastBuf)
	}
	
	
	toIdcName := reqHeader.Get("toIdcName")
    if httpTypeObject, result := findObject(toIdcName); result == true {
        config := kcpConfig
        httpid := gen_next_http_id()
        idx := uint16(httpid) % uint16(config.Conn)

        if httpTypeObject.muxes[idx].session.IsClosed() || (config.AutoExpire > 0 && time.Now().After(httpTypeObject.muxes[idx].ttl)) {
            httpTypeObject.muxes[idx].session = waitKcpConn()
            httpTypeObject.muxes[idx].ttl = time.Now().Add(time.Duration(config.AutoExpire) * time.Second)
        }   
        smuxSession := httpTypeObject.muxes[idx].session

        smuxStream, err := smuxSession.OpenStream()
        if err != nil {
            return
        }   
        defer smuxStream.Close()

 //       if req.Method == "CONNECT" {
 //           client.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n"))
 //       } else {
            smuxStream.Write(b.Bytes())
            log.Printf("%s", b.Bytes())
 //       }   

        // start tunnel
        p1die := make(chan struct{})
        go func() { io.Copy(client, smuxStream); close(p1die) }() 

        p2die := make(chan struct{})
        go func() { io.Copy(smuxStream, client); close(p2die) }() 

        // wait for tunnel termination
		select {
        case <-p1die:
        case <-p2die:
        }
    }

//	log.Printf("%s",b.Bytes())

/*
	req, _ := http.ReadRequest(bf)
	dump, _ := httputil.DumpRequest(req, true)
	log.Printf("%s",dump)
*/

}

func findObject(name string) (*kcpObject, bool) {
	if name == "" {
		return nil, false
	}
	if v, found := objects.Get(name); found {
		return v.(*kcpObject), true
	}
	return nil, false
}

func addObject(obj *kcpObject) {
	objects.Set(obj.name, obj)
}

func removeObject(name string) {
	objects.Remove(name)
}
