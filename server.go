package main

import (
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"

	"github.com/wirepair/netcode"
	"strings"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync/atomic"
	"time"
)

var webServerAddr string
var serverAddr string
var numServers int
var startingPort int
var maxClients int

//var runProfiler bool

var clientId uint64
var serverAddrs []net.UDPAddr

var httpServer *http.Server
var closeCh chan struct{}

const (
	PROTOCOL_ID          = 0x1122334455667788
	CONNECT_TOKEN_EXPIRY = 30
	TIMEOUT_SECONDS      = 1
	MAX_PACKET_BYTES     = 1220
	HOST_IP_ADDR 		 = "192.168.1.25"
	CLIENT_VERSION       = 0.01
	VERSION_CHECK_STR    = "VERSION - "
)

// serverKEY
var serverKey = []byte{0x60, 0x6a, 0xbe, 0x6e, 0xc9, 0x19, 0x10, 0xea,
	0x9a, 0x65, 0x62, 0xf6, 0x6f, 0x2b, 0x30, 0xe4,
	0x43, 0x71, 0xd6, 0x2c, 0xd1, 0x99, 0x27, 0x26,
	0x6b, 0x3c, 0x60, 0xf4, 0xb7, 0x15, 0xab, 0xa1}

func init() {
	flag.StringVar(&webServerAddr, "webaddr", ":8880", "the web server token supplier address to bind to")
	flag.IntVar(&numServers, "numservers", 3, "number of servers to start on successive ports")
	flag.IntVar(&startingPort, "port", 40000, "starting port number, increments by 1 for number of servers")
	flag.IntVar(&maxClients, "maxclients", 256, "number of clients per server")
	//flag.BoolVar(&runProfiler, "prof", false, "should we profile")
}

func main() {
	flag.Parse()
	closeCh = make(chan struct{}, 1)

	ctrlCloseCh := make(chan os.Signal, 1)
	signal.Notify(ctrlCloseCh, os.Interrupt)

	// initialize server addresses for connect tokens/listening
	serverAddrs = make([]net.UDPAddr, numServers)
	for i := 0; i < numServers; i += 1 {
		addr := net.UDPAddr{IP: net.ParseIP(HOST_IP_ADDR), Port: startingPort + i}
		serverAddrs[i] = addr
	}
	/*
		if runProfiler {
			p := profile.Start(profile.TraceProfile, profile.ProfilePath("."), profile.NoShutdownHook)
			defer p.Stop()
		}
	*/

	// start our netcode servers
	for i := 0; i < numServers; i += 1 {
		go clientServerVersionCheckLoop(closeCh, ctrlCloseCh, i)
	}

	// start our web server for generating and handing out connect tokens.
	http.HandleFunc("/token", serveToken)
	http.HandleFunc("/shutdown", serveShutdown)

	httpServer = &http.Server{Addr: webServerAddr}
	httpServer.ListenAndServe()
}


func clientServerVersionCheckLoop(closeCh chan struct{}, ctrlCloseCh chan os.Signal, index int) {
	serv := netcode.NewServer(&serverAddrs[index], serverKey, PROTOCOL_ID, maxClients)
	if err := serv.Init(); err != nil {
		log.Fatalf("error initializing server: %s\n", err)
	}

	if err := serv.Listen(); err != nil {
		log.Fatalf("error listening: %s\n", err)
	}

	// // payload := make([]byte, netcode.MAX_PAYLOAD_BYTES)
	// payload := make([]byte, 12)
	// payload_key_str := fmt.Sprintf("Client id: %d", clientId)
	// for i := 0; i < len(payload_key_str); i += 1 {
	// 	payload[i] = byte(payload_key_str[i])
	// }
	correct_version_payload := make([]byte, 4)
	correct_str := fmt.Sprintf("%f", CLIENT_VERSION)[:4]
	// log.Printf("CORRECT_STR: %s", correct_str)
	correct_runes := []rune(correct_str)
	for i := 0; i < len(correct_runes); i += 1 {
		correct_version_payload[i] = byte(correct_runes[i])
	}
	// log.Printf(string(correct_version_payload))
	serverTime := float64(0.0)
	delta := float64(1.0 / 60.0)
	deltaTime := time.Duration(delta * float64(time.Second))

	count := 0
	for {
		select {
		case <-ctrlCloseCh:
			shutdown(serv)
			return
		case <-closeCh:
			shutdown(serv)
			return
		default:
		}

		serv.Update(serverTime)
		for i := 0; i < 1; i += 1 {
			for {
				responsePayload, _ := serv.RecvPayload(i)
				if len(responsePayload) == 0 {
					break
				}
				if strings.Contains(string(responsePayload[:len(responsePayload)]), VERSION_CHECK_STR) {
					log.Printf("CORRECT CLIENT VERSION: %f", CLIENT_VERSION)
					serv.SendPayloadToClient(clientId, correct_version_payload, serverTime)
					// serv.SendPayloads(correct_version_payload, serverTime)
					break
				}
			}
		}

		// do simulation/process payload packets
		


		// send payloads to clients
		// serv.SendPayloads(payload, serverTime)

		time.Sleep(deltaTime)
		serverTime += deltaTime.Seconds()
		count += 1

	}
}

func shutdown(serv *netcode.Server) {
	log.Printf("shutting down server")
	serv.Stop()
	if err := httpServer.Close(); err != nil {
		log.Printf("error shutting down http server: %s\n", err)
	}
	return
}

// this is for the web server serving tokens...
type WebToken struct {
	ClientId     uint64 `json:"client_id"`
	ConnectToken string `json:"connect_token"`
}

func serveShutdown(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintf(w, "done")
	close(closeCh)
}

func serveToken(w http.ResponseWriter, r *http.Request) {
	clientId := incClientId() // safely increment the clientId

	tokenData, err := connectTokenGenerator(clientId, serverAddrs, netcode.VERSION_INFO, PROTOCOL_ID, CONNECT_TOKEN_EXPIRY, TIMEOUT_SECONDS, 0)
	if err != nil {
		fmt.Fprintf(w, "error")
		return
	}
	log.Printf("issuing new token for clientId: %d\n", clientId)
	webToken := WebToken{ClientId: clientId, ConnectToken: base64.StdEncoding.EncodeToString(tokenData)}
	json.NewEncoder(w).Encode(webToken)
	log.Printf("issued new token %d for clientId: %d\n", webToken, clientId)
}

func connectTokenGenerator(clientId uint64, serverAddrs []net.UDPAddr, versionInfo string, protocolId uint64, tokenExpiry uint64, timeoutSeconds int32, sequence uint64) ([]byte, error) {
	userData, err := netcode.RandomBytes(netcode.USER_DATA_BYTES)
	if err != nil {
		return nil, err
	}

	connectToken := netcode.NewConnectToken()
	if err := connectToken.Generate(clientId, serverAddrs, versionInfo, protocolId, tokenExpiry, timeoutSeconds, sequence, userData, serverKey); err != nil {
		return nil, err
	}

	return connectToken.Write()
}

func incClientId() uint64 {
	return atomic.AddUint64(&clientId, 1)
}