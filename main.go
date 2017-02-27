package main

import (
	"bufio"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/boxofrox/cctv-ptz/config"
	"github.com/docopt/docopt-go"
	"github.com/mikepb/go-serial"
	"github.com/simulatedsimian/joystick"
	"io"
	"math"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"
)

var (
	VERSION    string
	BUILD_DATE string
)

// pelco d byte names
const (
	SYNC      = 0
	ADDR      = 1
	COMMAND_1 = 2
	COMMAND_2 = 3
	DATA_1    = 4
	DATA_2    = 5
	CHECKSUM  = 6
)

type PelcoDMessage [7]byte

type DelayedMessage struct {
	Message PelcoDMessage
	Delay   time.Duration
}

const (
	AxisMax  = 32767
	MaxSpeed = 0x3f
)

type Axis struct {
	Index    int32
	Min      int32 // used for normalizing input -1.0 to 1.0
	Max      int32
	Deadzone int32
	Inverted bool // flips normalized input
}

var xbox = struct {
	LeftAxisX    Axis
	LeftAxisY    Axis
	RightAxisX   Axis
	RightAxisY   Axis
	LeftTrigger  Axis
	RightTrigger Axis
	DPadX        Axis
	DPadY        Axis
	LeftBumper   uint32
	RightBumper  uint32
	A            uint32
	B            uint32
	X            uint32
	Y            uint32
	Start        uint32
	Back         uint32
	XBox         uint32
}{
	Axis{0, -AxisMax, AxisMax, 8192, false}, // left analog stick
	Axis{1, -AxisMax, AxisMax, 8192, true},
	Axis{3, -AxisMax, AxisMax, 8192, false}, // right analog stick
	Axis{4, -AxisMax, AxisMax, 8192, true},
	Axis{2, -AxisMax, AxisMax, 1000, false}, // triggers
	Axis{5, -AxisMax, AxisMax, 1000, false},
	Axis{6, -AxisMax, AxisMax, 1000, false}, // directional pad
	Axis{7, -AxisMax, AxisMax, 1000, false},
	1 << 4, // bumpers
	1 << 5,
	1 << 0, // A
	1 << 1, // B
	1 << 2, // X
	1 << 3, // Y
	1 << 7, // start
	1 << 6, // back
	1 << 8, // xbox button
}

// map xbox controller to pan-tilt-zoom controls and misc app controls
var ptz = struct {
	// pan tilt zoom
	PanX      Axis
	PanY      Axis
	ZoomIn    uint32
	ZoomOut   uint32
	OpenIris  uint32
	CloseIris uint32
	OpenMenu  uint32

	// misc
	IncPelcoAddr uint32
	DecPelcoAddr uint32
	ResetTimer   uint32
	MarkLeft     Axis
	MarkRight    Axis
}{
	xbox.LeftAxisX,   // pan x
	xbox.RightAxisY,  // pan y
	xbox.LeftBumper,  // zoom in
	xbox.RightBumper, // zoom out
	xbox.A,           // open iris (enter)
	xbox.B,           // close iris
	xbox.Start,       // open menu

	xbox.Y,            // increment pelco address
	xbox.X,            // decrement pelco address
	xbox.Back,         // reset timer
	xbox.LeftTrigger,  // mark
	xbox.RightTrigger, // mark
}

func main() {
	var (
		err       error
		arguments map[string]interface{}
	)

	usage := `CCTV Pan-Tilt-Zoom via Xbox Controller

  Usage:
  cctv-ptz [-v] [-a ADDRESS] [-s FILE] [-j JOYSTICK] [-r FILE] [-b BAUD] [-m MAXSPEED]
  cctv-ptz playback [-a ADDRESS] [-s FILE] [-b BAUD] [-v]
  cctv-ptz -h
  cctv-ptz -V

  Options:
  -a, --address ADDRESS    - Pelco-D address 0-256. (default = 0)
  -b, --baud BAUD          - set baud rate of serial port. (default = 9600)
  -j, --joystick JOYSTICK  - use joystick NUM (e.g. /dev/input/jsNUM). (default = 0)
  -m, --maxspeed MAXSPEED  - set max speed setting 0-100. (default = 100)
  -s, --serial FILE        - assign serial port for rs485 output. (default = /dev/sttyUSB0)
  -r, --record FILE        - record rs485 commands to file. (default = /dev/null)
  -v, --verbose            - prints Pelco-D commands to stdout.
  -h, --help               - print this help message.
  -V, --version            - print version info.
  `

	arguments, err = docopt.Parse(usage, nil, true, version(), false)

	// fail if arguments failed to parse
	if err != nil {
		panic(err)
	}

	conf := config.Load(arguments)

	if arguments["playback"].(bool) {
		playback(conf)
	} else {
		interactive(conf)
	}
}

func createSerialOptions(conf config.Config) serial.Options {
	return serial.Options{
		Mode:        serial.MODE_WRITE,
		BitRate:     conf.BaudRate,
		DataBits:    8,
		StopBits:    1,
		Parity:      serial.PARITY_NONE,
		FlowControl: serial.FLOWCONTROL_NONE,
	}
}

func decodeMessage(text string) (PelcoDMessage, error) {
	var (
		bytes []byte
		err   error
	)

	message := PelcoDMessage{}
	if bytes, err = hex.DecodeString(text); err != nil {
		return message, err
	}

	copy(message[:], bytes)

	return message, nil
}

func interactive(conf config.Config) {
	var (
		record          *os.File
		tty             *serial.Port
		jsObserver      <-chan joystick.State
		err             error
		resetTimer      = true
		serialEnabled   = ("/dev/null" != conf.SerialPort)
		hasSerialAccess bool
	)

	stdinObserver := listenFile(os.Stdin)

	js, err := joystick.Open(conf.JoystickNumber)
	if err != nil {
		fmt.Fprintf(os.Stderr, "cctv-ptz: error opening joystick %d. %s\n", conf.JoystickNumber, err)

		jsObserver = listenNothing()
	} else {
		defer js.Close()

		fmt.Fprintf(os.Stderr, "Joystick port opened. /dev/input/js%d\n", conf.JoystickNumber)
		fmt.Fprintf(os.Stderr, "  Joystick Name: %s\n", js.Name())
		fmt.Fprintf(os.Stderr, "     Axis Count: %d\n", js.AxisCount())
		fmt.Fprintf(os.Stderr, "   Button Count: %d\n", js.ButtonCount())

		jsTicker := time.NewTicker(100 * time.Millisecond)
		jsObserver = listenJoystick(js, jsTicker)
	}

	hasSerialAccess, err = serialPortAvailable(conf.SerialPort)
	if err != nil {
		fmt.Fprintf(os.Stderr, "cctv-ptz: cannot open serial port (%s). %s\n", conf.SerialPort, err)
	}

	if serialEnabled && hasSerialAccess {
		ttyOptions := createSerialOptions(conf)

		tty, err = ttyOptions.Open(conf.SerialPort)
		if err != nil {
			fmt.Fprintf(os.Stderr, "cctz-ptz: unable to open tty: %s\n", conf.SerialPort)
			os.Exit(1)
		}
		defer tty.Close()

		printSerialPortInfo(conf, tty)
	} else {
		fmt.Fprintf(os.Stderr, "cctv-ptz: serial port disabled\n")
	}

	if "-" == conf.RecordFile {
		record = os.Stdout
	} else {
		if record, err = os.Create(conf.RecordFile); err != nil {
			panic(err)
		}
	}
	defer record.Close()

	// limit rate at which Pelco address may change via joystick
	allowAddressChange := make(chan struct{}, 1)
	allowAddressChange <- struct{}{} // prime channel to allow first address change

	startTime := time.Now()

	lastMessage := PelcoDMessage{}

	for {
		select {
		case <-stdinObserver:
			return
		case state := <-jsObserver:
			// adjust Pelco address
			if isPressed(state, ptz.DecPelcoAddr) {
				limitChange(allowAddressChange, func() { conf.Address -= 1 })
			} else if isPressed(state, ptz.IncPelcoAddr) {
				limitChange(allowAddressChange, func() { conf.Address += 1 })
			}

			// reset the clock if user presses Back
			if isPressed(state, ptz.ResetTimer) {
				resetTimer = true
			}

			if isMarkTriggered(state, ptz.MarkLeft) {
				fmt.Fprintf(record, "# Mark Left\n")
			}

			if isMarkTriggered(state, ptz.MarkRight) {
				fmt.Fprintf(record, "# Mark Right\n")
			}

			message := pelcoCreate()
			message = pelcoTo(message, conf.Address)
			message = joystickToPelco(message, state, conf.MaxSpeed)
			message = pelcoChecksum(message)

			if lastMessage != message {
				var millis int64

				if resetTimer {
					millis = 0
					resetTimer = false
					startTime = time.Now()
				} else {
					endTime := time.Now()
					millis = (endTime.Sub(startTime)).Nanoseconds() / 1E6
					startTime = endTime
				}

				if conf.Verbose {
					fmt.Printf("pelco-d %x %d\n", message, millis)
				} else {
					fmt.Fprintf(os.Stderr, "\033[Kpelco-d %x %d\r", message, millis)
				}
				fmt.Fprintf(record, "pelco-d %x %d\n", message, millis)

				if serialEnabled {
					tty.Write(message[:])
				}

				lastMessage = message
			}
		}
	}
}

func isPressed(state joystick.State, mask uint32) bool {
	return 0 != state.Buttons&mask
}

func joystickToPelco(buffer PelcoDMessage, state joystick.State, maxSpeed int32) PelcoDMessage {
	var zoom float32

	panX := normalizeAxis(state, ptz.PanX)
	panY := normalizeAxis(state, ptz.PanY)
	openIris := isPressed(state, ptz.OpenIris)
	closeIris := isPressed(state, ptz.CloseIris)
	openMenu := isPressed(state, ptz.OpenMenu)

	if isPressed(state, ptz.ZoomOut) {
		zoom = -1.0
	} else if isPressed(state, ptz.ZoomIn) {
		zoom = 1.0
	}

	buffer = pelcoApplyJoystick(buffer, panX, panY, zoom, openIris, closeIris, openMenu, maxSpeed)

	return buffer
}

func limitChange(allowAddressChange chan struct{}, proc func()) {
	select {
	case <-allowAddressChange:
		proc()

		// delay next signal to allow change
		go func() {
			<-time.After(125 * time.Millisecond)
			allowAddressChange <- struct{}{}
		}()
	default:
		// do nothing
	}
}

func listenFile(f io.Reader) <-chan []byte {
	io := make(chan []byte)
	scanner := bufio.NewScanner(f)

	go func() {
		defer close(io)

		for scanner.Scan() {
			bytes := scanner.Bytes()

			if len(bytes) == 0 {
				break
			}

			io <- bytes
		}
		if err := scanner.Err(); err != nil {
			panic(err)
		}
	}()

	return io
}

func listenJoystick(js joystick.Joystick, ticker *time.Ticker) <-chan joystick.State {
	io := make(chan joystick.State, 20)

	go func() {
		for range ticker.C {
			if state, err := js.Read(); err != nil {
				panic(err)
			} else {
				io <- state
			}
			time.Sleep(25 * time.Millisecond)
		}
	}()

	return io
}

func listenNothing() <-chan joystick.State {
	return make(chan joystick.State)
}

func isMarkTriggered(state joystick.State, axis Axis) bool {
	triggerValue := normalizeAxis(state, axis)

	return 0.5 < triggerValue
}

func normalizeAxis(state joystick.State, axis Axis) float32 {
	var (
		value    = float32(state.AxisData[axis.Index])
		deadzone = float32(axis.Deadzone)
		max      = float32(axis.Max)
	)

	if axis.Inverted {
		value = -value
	}

	if value > 0 && value < deadzone {
		value = 0
	} else if value > deadzone {
		value = (value - deadzone) / (max - deadzone)
	} else if value < 0 && value > -deadzone {
		value = 0
	} else if value < -deadzone {
		value = (value + deadzone) / (max - deadzone)
	}

	return value
}

func pelcoCreate() PelcoDMessage {
	buffer := PelcoDMessage{}

	buffer[SYNC] = 0xff

	return buffer
}

// should be last call before sending a pelco message
func pelcoChecksum(buffer PelcoDMessage) PelcoDMessage {
	buffer[CHECKSUM] = uint8(buffer[ADDR] + buffer[COMMAND_1] + buffer[COMMAND_2] + buffer[DATA_1] + buffer[DATA_2])

	return buffer
}

func pelcoTo(buffer PelcoDMessage, addr int) PelcoDMessage {
	buffer[ADDR] = uint8(addr)
	return buffer
}

func pelcoApplyJoystick(buffer PelcoDMessage, panX, panY, zoom float32, openIris, closeIris, openMenu bool, maxSpeed int32) PelcoDMessage {
	if openMenu {
		buffer[COMMAND_1] = 0x00
		buffer[COMMAND_2] = 0x03
		buffer[DATA_1] = 0x00
		buffer[DATA_2] = 0x5F

		return buffer
	}

	if panX > 0 {
		buffer[COMMAND_2] |= 1 << 1
	} else if panX < 0 {
		buffer[COMMAND_2] |= 1 << 2
	}

	// pan speed
	buffer[DATA_1] = uint8(float64(maxSpeed) * math.Abs(float64(panX)))

	if panY > 0 {
		buffer[COMMAND_2] |= 1 << 3
	} else if panY < 0 {
		buffer[COMMAND_2] |= 1 << 4
	}

	// tilt speed
	buffer[DATA_2] = uint8(float64(maxSpeed) * math.Abs(float64(panY)))

	if zoom > 0 {
		buffer[COMMAND_2] |= 1 << 5
	} else if zoom < 0 {
		buffer[COMMAND_2] |= 1 << 6
	}

	if openIris {
		buffer[COMMAND_1] |= 1 << 1
	} else if closeIris {
		buffer[COMMAND_1] |= 1 << 2
	}

	return buffer
}

func playback(conf config.Config) {
	var (
		message         PelcoDMessage
		tty             *serial.Port
		millis          uint64
		err             error
		serialEnabled   = ("/dev/null" != conf.SerialPort)
		hasSerialAccess bool
	)

	hasSerialAccess, err = serialPortAvailable(conf.SerialPort)
	if err != nil {
		fmt.Fprintf(os.Stderr, "cctv-ptz: cannot open serial port (%s). %s\n", conf.SerialPort, err)
	}

	if serialEnabled && hasSerialAccess {
		ttyOptions := createSerialOptions(conf)

		tty, err = ttyOptions.Open(conf.SerialPort)
		if err != nil {
			panic(err)
		}
		defer tty.Close()

		printSerialPortInfo(conf, tty)
	} else {
		fmt.Fprintf(os.Stderr, "Serial port disabled\n")
	}

	messageChannel := make(chan DelayedMessage)
	defer close(messageChannel)

	go sendDelayedMessages(messageChannel, tty, conf.Verbose)

	lineCount := 0
	lineScanner := bufio.NewScanner(os.Stdin)

	for lineScanner.Scan() {
		text := strings.TrimSpace(lineScanner.Text())

		if strings.HasPrefix(text, "#") {
			continue
		}

		words := strings.Fields(text)

		lineCount += 1

		if 3 > len(words) {
			fmt.Fprintf(os.Stderr, "cctv-ptz: error parsing playback. Too few fields.  Line %d: %s\n", lineCount, text)
			continue
		}

		if "pelco-d" != words[0] {
			fmt.Fprintf(os.Stderr, "cctv-ptz: error parsing playback. Invalid protocol %s.  Line %d: %s\n", words[0], lineCount, text)
			continue
		}

		if message, err = decodeMessage(words[1]); err != nil {
			fmt.Fprintf(os.Stderr, "cctv-ptz: error parsing playback. Invalid packet %s.  Line %d: %s\n", err.Error(), lineCount, text)
			continue
		}

		if millis, err = strconv.ParseUint(words[2], 10, 64); err != nil {
			fmt.Fprintf(os.Stderr, "cctv-ptz: error parsing playback. Invalid duration %s.  Line %d: %s\n", err.Error(), lineCount, text)
			continue
		}

		messageChannel <- DelayedMessage{message, time.Duration(millis) * time.Millisecond}

		if conf.Verbose {
			fmt.Fprintf(os.Stderr, "%s\n", text)
		}
	}
}

func printSerialPortInfo(conf config.Config, tty *serial.Port) {
	baud, err := tty.BitRate()
	if err != nil {
		panic(err)
	}

	data, err := tty.DataBits()
	if err != nil {
		panic(err)
	}

	stop, err := tty.StopBits()
	if err != nil {
		panic(err)
	}

	parity, err := tty.Parity()
	if err != nil {
		panic(err)
	}

	fmt.Fprintf(os.Stderr, "Serial port opened. %s\n", conf.SerialPort)
	fmt.Fprintf(os.Stderr, "        Name: %s\n", tty.Name())
	fmt.Fprintf(os.Stderr, "   Baud rate: %d\n", baud)
	fmt.Fprintf(os.Stderr, "   Data bits: %d\n", data)
	fmt.Fprintf(os.Stderr, "   Stop bits: %d\n", stop)
	fmt.Fprintf(os.Stderr, "      Parity: %d\n", parity)
}

func sendMessage(tty *serial.Port, message PelcoDMessage) {
	if nil != tty {
		tty.Write(message[:])
	}
}

func sendDelayedMessages(c <-chan DelayedMessage, tty *serial.Port, verbose bool) {
	var (
		pkg      DelayedMessage
		lastTime time.Time
	)

	// send first message without delay
	pkg = <-c
	sendMessage(tty, pkg.Message)
	lastTime = time.Now()

	// all other messages are delayed wrt preceeding messages
	for pkg = range c {
		time.Sleep(pkg.Delay)
		sendMessage(tty, pkg.Message)

		if verbose {
			duration := time.Now().Sub(lastTime) / 1E6
			delay := pkg.Delay / 1E6
			fmt.Fprintf(os.Stderr, "Sent %x after %d millis. target %d millis.  offset %d millis\n",
				pkg.Message, duration, delay, duration-delay)
		}

		lastTime = time.Now()
	}
}

func serialPortAvailable(serialPort string) (bool, error) {
	var err error

	goStat, err := os.Stat(serialPort)

	if os.IsNotExist(err) || os.IsPermission(err) {
		return false, err
	}

	euid := uint32(os.Geteuid())

	unixStat, ok := goStat.Sys().(*syscall.Stat_t)

	if !ok {
		return false, errors.New("cannot determine file ownership or permissions")
	}

	if euid == unixStat.Uid && 0 != (0x600&unixStat.Mode) {
		// we should have owner access!
		return true, nil
	}

	if 0 != (0x006 & unixStat.Mode) {
		// we should have other access!
		return true, nil
	}

	if 0 != (0x060 & unixStat.Mode) {
		groups, err := os.Getgroups()

		if err != nil {
			return false, err
		}

		// does any group for user match file's group?
		for _, gid := range groups {
			if uint32(gid) == unixStat.Gid {
				// we should have group access!
				return true, nil
			}
		}
	}

	return false, errors.New(fmt.Sprintf("access denied. uid (%d) gid (%d) mode (%o)", unixStat.Uid, unixStat.Gid, 0xfff & unixStat.Mode))
}

func version() string {
	return fmt.Sprintf("%s: version %s, build %s\n\n", os.Args[0], VERSION, BUILD_DATE)
}
