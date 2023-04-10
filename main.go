package main

import (
	"crypto/rand"
	"crypto/rsa"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

var key ssh.Signer

func init() {
	privateKey, err := rsa.GenerateKey(rand.Reader, 4096)
	if err != nil {
		log.Fatal(err)
	}
	key, _ = ssh.NewSignerFromKey(privateKey)
}

func main() {
	var (
		id       string
		password string
		port     int
	)
	flag.StringVar(&id, "id", "", "")
	flag.StringVar(&password, "password", "", "")
	flag.IntVar(&port, "port", 0, "")
	flag.Parse()

	config := ssh.ServerConfig{
		PasswordCallback: func(c ssh.ConnMetadata, pass []byte) (*ssh.Permissions, error) {
			if c.User() != id || string(pass) != password {
				return nil, errors.New("id/password mismatch")
			}
			return nil, nil
		},
	}
	config.AddHostKey(key)

	l, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		log.Fatal("failed to listen for connection", err)
	}
	log.Printf("Listening on %v", l.Addr())

	for {
		nConn, err := l.Accept()
		if err != nil {
			log.Printf("l.Accept(): %v", err)
			continue
		}

		_, chs, reqs, err := ssh.NewServerConn(nConn, &config)
		if err != nil {
			log.Printf("ssh.NewServerConn(): %v", err)
			continue
		}

		go ssh.DiscardRequests(reqs)

		for ch := range chs {
			if ch.ChannelType() != "session" {
				if err := ch.Reject(ssh.UnknownChannelType, "unknown channel type"); err != nil {
					log.Printf("ch.Reject(): %v", err)
				}
				continue
			}

			channel, requests, err := ch.Accept()
			if err != nil {
				log.Printf("ch.Accept(): %v", err)
				continue
			}

			go func(in <-chan *ssh.Request) {
				for req := range in {
					if err := req.Reply(req.Type == "subsystem" && string(req.Payload[4:]) == "sftp", nil); err != nil {
						log.Printf("req.Reply(): %v", err)
					}
				}
			}(requests)

			server, err := sftp.NewServer(channel)
			if err != nil {
				log.Printf("sftp.NewServer(): %v", err)
				continue
			}

			go func() {
				log.Printf("server.Serve(): %s", nConn.RemoteAddr())
				switch err := server.Serve(); {
				case err == nil:
					break
				case errors.Is(err, io.EOF):
					_ = server.Close()
					log.Printf("server.Close(): %s, %v", nConn.RemoteAddr(), err)
				default:
					log.Fatalf("server.Serve(): %v", err)
				}
			}()
		}
	}
}
