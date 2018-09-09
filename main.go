package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"

	pubnub "github.com/pubnub/go"
)

var pn *pubnub.PubNub

func init() {
	pubnubConfig := LoadConfiguration(`configs/pubnub.json`)

	config := pubnub.NewConfig()
	config.SecretKey = pubnubConfig.SecretKey
	config.SubscribeKey = pubnubConfig.SubscribeKey
	config.PublishKey = pubnubConfig.PublishKey

	pn = pubnub.NewPubNub(config)
}

func main() {
	listener := pubnub.NewListener()
	forever := make(chan bool)

	go func() {
		for {
			select {
			case status := <-listener.Status:
				switch status.Category {
				case pubnub.PNConnectedCategory:
					fmt.Println("Connected to cactuspi")
				case pubnub.PNUnknownCategory:
					fmt.Println("Unable to connect to cactuspi")
				}
			case message := <-listener.Message:
				fmt.Println(message.Message)
				md := message.UserMetadata.(map[string]interface{})
				switch md["name"] {
				case "subway":
					fmt.Println("subway")
				case "weather":
					fmt.Println("weather")
				}
			case <-listener.Presence:
			}
		}
	}()

	pn.AddListener(listener)

	pn.Subscribe().
		Channels([]string{"cactuspi"}).
		Execute()

	log.Printf(" [*] Waiting for messages. To exit press CTRL+C")
	<-forever
}

type UserMeta interface {
	priority() string
	repeat() string
	name() string
	duration() string
}

type Config struct {
	SubscribeKey string   `json:"subscribeKey"`
	SecretKey    string   `json:"secretKey"`
	PublishKey   string   `json:"publishKey"`
	Channels     []string `json:"channels"`
}

func LoadConfiguration(file string) Config {
	var config Config
	configFile, err := os.Open(file)
	defer configFile.Close()
	if err != nil {
		fmt.Println(err.Error())
	}
	jsonParser := json.NewDecoder(configFile)
	jsonParser.Decode(&config)
	return config
}
