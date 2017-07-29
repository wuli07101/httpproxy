package main

import (
	"bufio"
	"bytes"
	"fmt"
	"github.com/xtaci/smux"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"
)

// handle multiplex-ed connection
func handleMuxHttp(conn io.ReadWriteCloser, config *Config) {
	// stream multiplex
	smuxConfig := smux.DefaultConfig()
	smuxConfig.MaxReceiveBuffer = config.SockBuf
	smuxConfig.KeepAliveInterval = time.Duration(config.KeepAlive) * time.Second

	mux, err := smux.Server(conn, smuxConfig)
	if err != nil {
		log.Println(err)
		return
	}
	defer mux.Close()
	for {
		p1, err := mux.AcceptStream()
		if err != nil {
			log.Println(err)
			return
		}
		go handleHttpProxy_2(p1)
	}
}

func handleHttpProxy_1(p1 io.ReadWriteCloser) {
	log.Println("stream opened")
	defer log.Println("stream closed")
	defer p1.Close()

	var b [1024]byte
	n, err := p1.Read(b[:])

	if err != nil {
		log.Println(err)
		return
	}

	var method, host, address string
	log.Println(string(b[:bytes.IndexByte(b[:], '\n')]))
	fmt.Sscanf(string(b[:bytes.IndexByte(b[:], '\n')]), "%s%s", &method, &host)
	hostPortURL, err := url.Parse(host)
	if err != nil {
		log.Println(err)
		return
	}

	if hostPortURL.Opaque == "443" { //https访问
		address = hostPortURL.Scheme + ":443"
	} else { //http访问
		if strings.Index(hostPortURL.Host, ":") == -1 { //host不带端口， 默认80
			address = hostPortURL.Host + ":80"
		} else {
			address = hostPortURL.Host
		}
	}
	//获得了请求的host和port，就开始拨号吧
	p2, err := net.Dial("tcp", address)
	if err != nil {
		log.Println(err)
		return
	}
	if method == "CONNECT" {
		fmt.Fprint(p1, "HTTP/1.1 200 Connection established\r\n\r\n")
	} else {
		log.Printf("%s",b[:n])
		p2.Write(b[:n])
	}

	// start tunnel
	p1die := make(chan struct{})
	go func() { io.Copy(p1, p2); close(p1die) }()

	p2die := make(chan struct{})
	go func() { io.Copy(p2, p1); close(p2die) }()

	// wait for tunnel termination
	select {
	case <-p1die:
	case <-p2die:
	}
}

func handleHttpProxy_2(client io.ReadWriteCloser) {
	log.Println("stream opened")
	defer log.Println("stream closed")
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
	//获得了请求的host和port，就开始拨号吧
	server, err := net.Dial("tcp", address)
	if err != nil {
		log.Println(err)
		return
	}
	if req.Method == "CONNECT" {
		client.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n"))
	} else {
<<<<<<< HEAD
        dump, _ := httputil.DumpRequestOut(req, true)
=======
		dump, _ := httputil.DumpRequestOut(req, true)
>>>>>>> 684cdfe92f299083987853590589e21297db38fa
		server.Write(dump)
		log.Printf("%s",dump)
	}

	// start tunnel
	p1die := make(chan struct{})
	go func() { io.Copy(client, server); close(p1die) }()

	p2die := make(chan struct{})
	go func() { io.Copy(server, client); close(p2die) }()

	// wait for tunnel termination
	select {
	case <-p1die:
	case <-p2die:
	}
}
