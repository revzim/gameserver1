package main

import (
	"encoding/base64"
	"encoding/json"
	"flag"
	"log"
	"math/rand"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
	"fmt"

	"github.com/wirepair/netcode"
)
const (
		CLIENT_VERSION       = 0.01
		VERSION_CHECK_STR    = "VERSION - "
)

var totalPayloadCount uint64
var totalSendCount uint64
var totalTickCount uint64

var tokenUrl string
var numClients int
var game_over bool

func init() {
	flag.StringVar(&tokenUrl, "url", "http://192.168.1.25:8880/token", "site that gives out free tokens")
	flag.IntVar(&numClients, "num", 1, "number of clients to run concurrently")
}

func main() {
	flag.Parse()
	wg := &sync.WaitGroup{}

	for i := 0; i < numClients; i += 1 {
		wg.Add(1)
		token, id := getConnectToken()
		go clientServerVersionCheckLoop(wg, id, token)
	}
	wg.Wait()
	log.Printf("%d clients sent %d packets, recv'd %d payloads in %d ticks\n", numClients, totalSendCount, totalPayloadCount, totalTickCount)
}


func clientServerVersionCheckLoop(wg *sync.WaitGroup, id uint64, connectToken *netcode.ConnectToken) {
	client_server_verified := false
	clientTime := float64(0)
	delta := float64(1.0 / 60.0)
	deltaTime := time.Duration(delta * float64(time.Second))

	c := netcode.NewClient(connectToken)
	c.SetId(id)

	if err := c.Connect(); err != nil {
		log.Fatalf("error connecting: %s\n", err)
	}

	log.Printf("Client Local Addr: %s connected from Remote Addr: %s", c.LocalAddr(), c.RemoteAddr())

	//PACKET VERSION DATA
	packetData := make([]byte, len(VERSION_CHECK_STR) + 4)
	for i := 0; i < len(VERSION_CHECK_STR); i += 1 {
		packetData[i] = byte(VERSION_CHECK_STR[i])
	}
	client_version_byte_arr := []byte(fmt.Sprintf("%f", CLIENT_VERSION))

	packetData[10] = client_version_byte_arr[0]
	packetData[11] = client_version_byte_arr[1]
	packetData[12] = client_version_byte_arr[2]
	packetData[13] = client_version_byte_arr[3]
	
	log.Printf("PACKET DATA BEING PREPARED - %s", string(packetData))


	count := 0
	sendCount := 0
	ticks := 0

	// randomize start time so we don't flood ourselves/server
	time.Sleep(time.Duration(rand.Intn(100)) * time.Millisecond)

	// loop to check client server version
	for {

		if client_server_verified {
			// log.Printf("client[%d] exiting recv'd %d payloads... from %d ticks", id, count, ticks)
			atomic.AddUint64(&totalTickCount, uint64(ticks))
			atomic.AddUint64(&totalPayloadCount, uint64(count))
			atomic.AddUint64(&totalSendCount, uint64(sendCount))
			log.Printf("Client[%d] verified version with server.\nExiting.", id)
			return

			// wg.Done()
			// return
		}

		if game_over {
			atomic.AddUint64(&totalTickCount, uint64(ticks))
			atomic.AddUint64(&totalPayloadCount, uint64(count))
			atomic.AddUint64(&totalSendCount, uint64(sendCount))
			log.Printf("Client[%d] game over.\nExiting.", id)
			wg.Done()
			return
		}

		c.Update(clientTime)
		if c.GetState() == netcode.StateConnected {
			c.SendData(packetData)
			log.Printf("SENT PACKET DATA. -- %s", string(packetData))
			sendCount++
			// break
		}

		//PAYLOAD SENT FROM SERVER PACKETS
		for {
			if payload, _ := c.RecvData(); payload == nil {
				break
			} else {
				log.Printf("PAYLOAD: %s", string(payload))
				if string(payload) == fmt.Sprintf("%f", CLIENT_VERSION)[:4] {
						log.Printf("CLIENT VERSION -> SERVER VERIFICATION!")
						count++
						client_server_verified = true
				}
			}
		}
		time.Sleep(deltaTime)
		clientTime += deltaTime.Seconds()
		ticks++
	}

}

// this is from the web server serving tokens...
type WebToken struct {
	ClientId     uint64 `json:"client_id"`
	ConnectToken string `json:"connect_token"`
}

func getConnectToken() (*netcode.ConnectToken, uint64) {

	resp, err := http.Get(tokenUrl)
	if err != nil {
		log.Fatalf("error getting token from %s: %s\n", tokenUrl, err)
	}
	defer resp.Body.Close()
	webToken := &WebToken{}

	if err := json.NewDecoder(resp.Body).Decode(webToken); err != nil {
		log.Fatalf("error decoding web token: %s\n", err)
	}
	log.Printf("Got token for clientId: %d\n", webToken.ClientId)

	tokenBuffer, err := base64.StdEncoding.DecodeString(webToken.ConnectToken)
	if err != nil {
		log.Fatalf("error decoding connect token: %s\n", err)
	}

	token, err := netcode.ReadConnectToken(tokenBuffer)
	if err != nil {
		log.Fatalf("error reading connect token: %s\n", err)
	}
	return token, webToken.ClientId
}