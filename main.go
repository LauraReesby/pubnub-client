package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"io/ioutil"
	"log"
	"os"
	"strings"
	"time"

	"github.com/disintegration/imaging"
	"github.com/golang/freetype"
	"github.com/golang/freetype/truetype"
	"github.com/mcuadros/go-rpi-rgb-led-matrix"
	pubnub "github.com/pubnub/go"
)

var (
	backgroundWidth  = 64
	backgroundHeight = 32
	utf8FontFile     = "Agane_55.ttf"
	utf8FontSize     = float64(12.0)
	spacing          = float64(1)
	dpi              = float64(72)
	ctx              = new(freetype.Context)
	utf8Font         = new(truetype.Font)
	red              = color.RGBA{255, 0, 0, 255}
	blue             = color.RGBA{0, 0, 255, 255}
	white            = color.RGBA{255, 255, 255, 255}
	black            = color.RGBA{0, 0, 0, 255}
	background       *image.RGBA
	tk               *rgbmatrix.ToolKit

	rows                     = flag.Int("led-rows", 32, "number of rows supported")
	cols                     = flag.Int("led-cols", 32, "number of columns supported")
	parallel                 = flag.Int("led-parallel", 1, "number of daisy-chained panels")
	chain                    = flag.Int("led-chain", 2, "number of displays daisy-chained")
	show_refresh             = flag.Bool("led-show-refresh", false, "Show refresh rate.")
	inverse_colors           = flag.Bool("led-inverse", false, "Switch if your matrix has inverse colors on.")
	disable_hardware_pulsing = flag.Bool("led-no-hardware-pulse", true, "Don't use hardware pin-pulse generation.")
	brightness               = flag.Int("brightness", 30, "brightness (0-100)")
	hardware_mapping         = flag.String("led-gpio-mapping", "adafruit-hat", "Name of GPIO mapping used.")
	img                      = flag.String("image", "utf8text.png", "image path")
	rotate                   = flag.Int("rotate", 0, "rotate angle, 90, 180, 270")

	pn *pubnub.PubNub
)

func init() {
	pubnubConfig := LoadConfiguration(`configs/pubnub.json`)

	config := pubnub.NewConfig()
	config.SecretKey = pubnubConfig.SecretKey
	config.SubscribeKey = pubnubConfig.SubscribeKey
	config.PublishKey = pubnubConfig.PublishKey

	pn = pubnub.NewPubNub(config)

	flag.Parse()
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

				tk.Close()
				md := message.UserMetadata.(map[string]interface{})
				msg := message.Message.(string)
				s := strings.Split(msg, "\n")

				switch md["name"] {
				case "subway":
					fmt.Println("subway")
					CreateImage(s)
					DisplayImage()
				case "weather":
					fmt.Println("weather")
					CreateImage(s)
					DisplayImage()
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

func CreateImage(subwayText []string) bool {
	fontBytes, err := ioutil.ReadFile(utf8FontFile)
	if err != nil {
		fmt.Println(err)
		return false
	}

	utf8Font, err = freetype.ParseFont(fontBytes)
	if err != nil {
		fmt.Println(err)
		return false
	}

	fontForeGroundColor, fontBackGroundColor := image.NewUniform(blue), image.NewUniform(black)
	background = image.NewRGBA(image.Rect(0, 0, backgroundWidth, backgroundHeight))
	draw.Draw(background, background.Bounds(), fontBackGroundColor, image.ZP, draw.Src)

	ctx = freetype.NewContext()
	ctx.SetDPI(dpi) //screen resolution in Dots Per Inch
	ctx.SetFont(utf8Font)
	ctx.SetFontSize(utf8FontSize) //font size in points
	ctx.SetClip(background.Bounds())
	ctx.SetDst(background)
	ctx.SetSrc(fontForeGroundColor)

	UTF8text := subwayText

	// Draw the text to the background
	pt := freetype.Pt(2, 2+int(ctx.PointToFixed(utf8FontSize)>>6))

	// not all utf8 fonts are supported by wqy-zenhei.ttf
	// use your own language true type font file if your language cannot be printed

	for _, str := range UTF8text {
		_, err := ctx.DrawString(str, pt)
		if err != nil {
			fmt.Println(err)
			return false
		}
		pt.Y += ctx.PointToFixed(utf8FontSize * spacing)
	}

	// Save
	outFile, err := os.Create("utf8text.png")
	if err != nil {
		fmt.Println(err)
		return false
	}
	defer outFile.Close()
	buff := bufio.NewWriter(outFile)

	err = png.Encode(buff, background)
	if err != nil {
		fmt.Println(err)
		return false
	}

	// flush everything out to file
	err = buff.Flush()
	if err != nil {
		fmt.Println(err)
		return false
	}
	fmt.Println("Save to utf8text.png")

	return true
}

func DisplayImage() bool {
	f, err := os.Open(*img)
	fatal(err)

	config := &rgbmatrix.DefaultConfig
	config.Rows = *rows
	config.Cols = *cols
	config.Parallel = *parallel
	config.ChainLength = *chain
	config.ShowRefreshRate = *show_refresh
	config.InverseColors = *inverse_colors
	config.DisableHardwarePulsing = *disable_hardware_pulsing
	config.Brightness = *brightness
	config.HardwareMapping = *hardware_mapping

	m, err := rgbmatrix.NewRGBLedMatrix(config)
	fatal(err)

	tk = rgbmatrix.NewToolKit(m)
	defer tk.Close()

	switch *rotate {
	case 90:
		tk.Transform = imaging.Rotate90
	case 180:
		tk.Transform = imaging.Rotate180
	case 270:
		tk.Transform = imaging.Rotate270
	}

	durationMinute := 90 * time.Minute

	loadedImage, err := png.Decode(f)

	err = tk.PlayImage(loadedImage, durationMinute)
	fatal(err)

	return true
}

func fatal(err error) {
	if err != nil {
		panic(err)
	}
}
