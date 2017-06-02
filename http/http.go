package main

import (
	"bufio"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"strings"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	l, err := net.Listen("tcp", ":1081")
	if err != nil {
		log.Panic(err)
	}

	for {
		client, err := l.Accept()
		if err != nil {
			log.Panic(err)
		}

		go handleClientRequest(client)
	}
}

func handleClientRequest(client net.Conn) {
	if client == nil {
		return
	}
	defer client.Close()

	bf := bufio.NewReader(client)
	req, err := http.ReadRequest(bf)
	if err != nil {
		log.Println(err)
		return
	}

	var address string
	if strings.Index(req.URL.Host, ":") == -1 { //host不带端口， 默认80
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
		dump, _ := httputil.DumpRequest(req, true)
		server.Write(dump)
	}
	//进行转发
	go io.Copy(server, client)
	io.Copy(client, server)
}
