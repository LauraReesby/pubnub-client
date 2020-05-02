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
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/disintegration/imaging"
	"github.com/golang/freetype"
	"github.com/golang/freetype/truetype"
	rgbmatrix "github.com/mcuadros/go-rpi-rgb-led-matrix"
	pubnub "github.com/pubnub/go"
)

var (
	backgroundWidth   = 64
	backgroundHeight  = 32
	utf8FontFile      = "assets/Agane_55.ttf"
	utf8FontSize      = float64(12.0)
	utf8FontSizeSmall = float64(9.5)
	spacing           = float64(1)
	dpi               = float64(72)
	ctx               = new(freetype.Context)
	utf8Font          = new(truetype.Font)
	red               = color.RGBA{255, 0, 0, 255}
	blue              = color.RGBA{0, 0, 255, 255}
	green             = color.RGBA{0, 255, 0, 0}
	white             = color.RGBA{255, 255, 255, 255}
	black             = color.RGBA{0, 0, 0, 255}
	textImage         *image.RGBA
	tk                *rgbmatrix.ToolKit

	rows                   = flag.Int("led-rows", 32, "number of rows supported")
	cols                   = flag.Int("led-cols", 32, "number of columns supported")
	parallel               = flag.Int("led-parallel", 1, "number of daisy-chained panels")
	chain                  = flag.Int("led-chain", 2, "number of displays daisy-chained")
	showRefresh            = flag.Bool("led-show-refresh", false, "Show refresh rate.")
	inverseColors          = flag.Bool("led-inverse", false, "Switch if your matrix has inverse colors on.")
	disableHardwarePulsing = flag.Bool("led-no-hardware-pulse", true, "Don't use hardware pin-pulse generation.")
	brightness             = flag.Int("brightness", 90, "brightness (0-100)")
	hardwareMapping        = flag.String("led-gpio-mapping", "adafruit-hat", "Name of GPIO mapping used.")
	img                    = flag.String("image", "assets/utf8text.png", "image path")
	rotate                 = flag.Int("rotate", 0, "rotate angle, 90, 180, 270")
	pwmBits                = flag.Int("pwmBits", 5, "pwmBits")
	pwmlsbNanoseconds      = flag.Int("pwmlsbNanoseconds", 70, "pwmlsbNanoseconds")

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

				if tk != nil {
					fmt.Println("Closing open canvas")
					tk.Close()
				}
				md := message.UserMetadata.(map[string]interface{})
				msg := message.Message.(string)

				switch md["name"] {
				case "subway":
					s := strings.Split(msg, "\n")
					prio := int(md["priority"].(float64))
					fmt.Println("subway, delay: " + strconv.Itoa(prio))
					CreateImage(s, prio)
					DisplayImage()
				case "weather":
					s := strings.Split(msg, "\n")
					fmt.Println("weather")
					CreateWeatherImage(s, md["priority"].(string))
					DisplayImage()
				case "covid":
					s := strings.Split(msg, ",")
					fmt.Println("covid")
					fmt.Println(s[0], s[1], s[2], s[3])
					CreateCovidImage(s)
					DisplayImage()
				default:
					fmt.Println("message not supported")
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

func CreateImage(subwayText []string, delay int) bool {
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
	textImage = image.NewRGBA(image.Rect(0, 0, backgroundWidth, backgroundHeight))
	draw.Draw(textImage, textImage.Bounds(), fontBackGroundColor, image.ZP, draw.Src)

	ctx = freetype.NewContext()
	ctx.SetDPI(dpi) //screen resolution in Dots Per Inch
	ctx.SetFont(utf8Font)
	ctx.SetFontSize(utf8FontSize) //font size in points
	ctx.SetClip(textImage.Bounds())
	ctx.SetDst(textImage)
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
	outFile, err := os.Create("assets/textImage.png")
	if err != nil {
		fmt.Println(err)
		return false
	}
	defer outFile.Close()
	buff := bufio.NewWriter(outFile)

	err = png.Encode(buff, textImage)
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

	src, err := imaging.Open("assets/textImage.png")
	if err != nil {
		fatal(err)
	}

	delayImage := ""
	if delay == 1 {
		delayImage = "assets/green-light.png"
	} else if delay == 2 {
		delayImage = "assets/yellow-light.png"
	} else {
		delayImage = "assets/red-light.png"
	}

	src2, err := imaging.Open(delayImage)
	if err != nil {
		fatal(err)
	}

	r2 := image.Rect(12, 0, backgroundWidth, backgroundHeight)
	finalImg := image.NewRGBA(image.Rect(0, 0, backgroundWidth, backgroundHeight))
	draw.Draw(finalImg, src2.Bounds(), src2, image.Point{0, 0}, draw.Src)
	draw.Draw(finalImg, r2, src, image.Point{0, 0}, draw.Src)

	finalFile, err := os.Create("assets/utf8text.png")
	if err != nil {
		fatal(err)
	}
	defer finalFile.Close()

	buffFinal := bufio.NewWriter(finalFile)
	err = png.Encode(buffFinal, finalImg)
	if err != nil {
		fatal(err)
	}

	// flush everything out to file
	err = buffFinal.Flush()
	if err != nil {
		fatal(err)
	}
	fmt.Println("Save to assets/utf8text.png")
	return true
}

func CreateWeatherImage(text []string, iconUrl string) bool {
	fontBytes, err := ioutil.ReadFile(utf8FontFile)
	if err != nil {
		fatal(err)
	}

	utf8Font, err = freetype.ParseFont(fontBytes)
	if err != nil {
		fatal(err)
	}

	fontForeGroundColor, fontBackGroundColor := image.NewUniform(blue), image.NewUniform(black)
	textImage = image.NewRGBA(image.Rect(0, 0, 32, backgroundHeight))
	draw.Draw(textImage, textImage.Bounds(), fontBackGroundColor, image.ZP, draw.Src)

	utf8FontSize = float64(11.0)
	ctx = freetype.NewContext()
	ctx.SetDPI(dpi) //screen resolution in Dots Per Inch
	ctx.SetFont(utf8Font)
	ctx.SetFontSize(utf8FontSize) //font size in points
	ctx.SetClip(textImage.Bounds())
	ctx.SetDst(textImage)
	ctx.SetSrc(fontForeGroundColor)

	UTF8text := text

	// Draw the text to the background
	pt := freetype.Pt(1, 2+int(ctx.PointToFixed(utf8FontSize)>>6))

	// not all utf8 fonts are supported by wqy-zenhei.ttf
	// use your own language true type font file if your language cannot be printed

	for _, str := range UTF8text {
		_, err := ctx.DrawString(str, pt)
		if err != nil {
			fatal(err)
		}
		pt.Y += ctx.PointToFixed(utf8FontSize * spacing)
	}

	// Save
	outFile, err := os.Create("assets/textImage.png")
	if err != nil {
		fatal(err)
	}
	defer outFile.Close()

	buff := bufio.NewWriter(outFile)
	err = png.Encode(buff, textImage)
	if err != nil {
		fatal(err)
	}

	// flush everything out to file
	err = buff.Flush()
	if err != nil {
		fatal(err)
	}

	outFile.Close()

	// Create the file
	out, err := os.Create("assets/weatherIcon.png")
	if err != nil {
		fatal(err)
	}
	defer out.Close()

	// Get the data
	resp, err := http.Get(iconUrl)
	if err != nil {
		fatal(err)
	}
	defer resp.Body.Close()

	// Write the body to file
	_, err = io.Copy(out, resp.Body)
	if err != nil {
		fatal(err)
	}

	src, err := imaging.Open("assets/weatherIcon.png")
	if err != nil {
		fatal(err)
	}
	dstImage128 := imaging.Resize(src, 36, 36, imaging.Lanczos)
	if strings.HasSuffix(iconUrl, "50d.png") || strings.HasSuffix(iconUrl, "50n.png") || strings.HasSuffix(iconUrl, "01n.png") {
		dstImage128 = imaging.Invert(dstImage128)
	}

	// Save the resulting image as png.
	err = imaging.Save(dstImage128, "assets/weatherIconResize.png")
	if err != nil {
		fatal(err)
	}

	src2, err := imaging.Open("assets/textImage.png")
	if err != nil {
		fatal(err)
	}

	r2 := image.Rect(30, 0, backgroundWidth, backgroundHeight)
	finalImg := image.NewRGBA(image.Rect(0, 0, backgroundWidth, backgroundHeight))
	draw.Draw(finalImg, src2.Bounds(), src2, image.Point{0, 0}, draw.Src)
	draw.Draw(finalImg, r2, dstImage128, image.Point{0, 0}, draw.Src)

	finalFile, err := os.Create("assets/utf8text.png")
	if err != nil {
		fatal(err)
	}
	defer finalFile.Close()

	buffFinal := bufio.NewWriter(finalFile)
	err = png.Encode(buffFinal, finalImg)
	if err != nil {
		fatal(err)
	}

	// flush everything out to file
	err = buffFinal.Flush()
	if err != nil {
		fatal(err)
	}
	fmt.Println("Save to assets/utf8text.png")
	return true
}

func CreateCovidImage(covidText []string) bool {
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
	textImage = image.NewRGBA(image.Rect(0, 0, backgroundWidth, backgroundHeight))
	draw.Draw(textImage, textImage.Bounds(), fontBackGroundColor, image.ZP, draw.Src)

	ctx = freetype.NewContext()
	ctx.SetDPI(dpi) //screen resolution in Dots Per Inch
	ctx.SetFont(utf8Font)
	ctx.SetFontSize(utf8FontSizeSmall) //font size in points
	ctx.SetClip(textImage.Bounds())
	ctx.SetDst(textImage)
	ctx.SetSrc(fontForeGroundColor)

	var textArray [2]string
	textArray[0] = "US:" + covidText[2]
	textArray[1] = "NY:" + covidText[0]
	UTF8text := textArray

	// Draw the text to the background
	pt := freetype.Pt(2, 2+int(ctx.PointToFixed(utf8FontSizeSmall)>>6))

	// not all utf8 fonts are supported by wqy-zenhei.ttf
	// use your own language true type font file if your language cannot be printed

	for _, str := range UTF8text {
		_, err := ctx.DrawString(str, pt)
		if err != nil {
			fmt.Println(err)
			return false
		}
		pt.Y += ctx.PointToFixed(utf8FontSizeSmall * spacing)
	}

	// Save
	outFile, err := os.Create("assets/textImage.png")
	if err != nil {
		fmt.Println(err)
		return false
	}
	defer outFile.Close()
	buff := bufio.NewWriter(outFile)

	err = png.Encode(buff, textImage)
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

	src, err := imaging.Open("assets/textImage.png")
	if err != nil {
		fatal(err)
	}

	delayImage := "assets/thermometer.png"

	src2, err := imaging.Open(delayImage)
	if err != nil {
		fatal(err)
	}

	r2 := image.Rect(10, 0, backgroundWidth, backgroundHeight)
	finalImg := image.NewRGBA(image.Rect(0, 0, backgroundWidth, backgroundHeight))
	draw.Draw(finalImg, src2.Bounds(), src2, image.Point{0, 0}, draw.Src)
	draw.Draw(finalImg, r2, src, image.Point{0, 0}, draw.Src)

	finalFile, err := os.Create("assets/utf8text.png")
	if err != nil {
		fatal(err)
	}
	defer finalFile.Close()

	buffFinal := bufio.NewWriter(finalFile)
	err = png.Encode(buffFinal, finalImg)
	if err != nil {
		fatal(err)
	}

	// flush everything out to file
	err = buffFinal.Flush()
	if err != nil {
		fatal(err)
	}
	fmt.Println("Save to assets/utf8text.png")
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
	config.ShowRefreshRate = *showRefresh
	config.InverseColors = *inverseColors
	config.DisableHardwarePulsing = *disableHardwarePulsing
	config.HardwareMapping = *hardwareMapping
	config.Brightness = *brightness
	config.PWMBits = *pwmBits
	config.PWMLSBNanoseconds = *pwmlsbNanoseconds

	m, err := rgbmatrix.NewRGBLedMatrix(config)
	fatal(err)
	tk = rgbmatrix.NewToolKit(m)

	switch *rotate {
	case 90:
		tk.Transform = imaging.Rotate90
	case 180:
		tk.Transform = imaging.Rotate180
	case 270:
		tk.Transform = imaging.Rotate270
	}

	loadedImage, err := png.Decode(f)

	err = tk.PlayImage(loadedImage, (5 * time.Second))
	fatal(err)

	return true
}

func fatal(err error) {
	if err != nil {
		panic(err)
	}
}
