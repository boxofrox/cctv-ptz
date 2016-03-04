package main

import (
	"bufio"
	"fmt"
	"github.com/boxofrox/cctv-ptz/config"
	"github.com/docopt/docopt-go"
	"github.com/mikepb/go-serial"
	"github.com/simulatedsimian/joystick"
	"io"
	"math"
	"os"
	"time"
)

// Controller Layout
//
// Controller                   PelcoD
// ----------                   ------
// Left Analog                  Pan w/ Speed
//   Up                         Up
//   Down                       Down
//   Left                       n/a
//   Right                      n/a
// Right Analog
//   Up                         n/a
//   Down                       n/a
//   Left                       Left
//   Right                      Right
// A                            Iris Open
// B                            Iris Close
// X                            Decrement Address
// Y                            Increment Address
// Left Bumper                  Zoom Out
// Right Bumper                 Zoom In
// Start                        Menu (Go to Preset 95)
// Back                         Reset recording start time

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

const AxisMax = 32767

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
	Axis{0, -AxisMax, AxisMax, 8192, false}, // left axis
	Axis{1, -AxisMax, AxisMax, 8192, true},
	Axis{3, -AxisMax, AxisMax, 8192, false}, // right axis
	Axis{4, -AxisMax, AxisMax, 8192, true},
	Axis{2, -AxisMax, AxisMax, 1000, false}, // triggers
	Axis{5, -AxisMax, AxisMax, 1000, false},
	Axis{6, -AxisMax, AxisMax, 1000, false}, // dpad
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

func main() {
	var (
		err       error
		arguments map[string]interface{}
	)

	usage := `CCTV Pan-Tilt-Zoom via Xbox Controller

Usage:
    cctv-ptz [-a ADDRESS] [-s FILE] [-j JOYSTICK] [-r FILE] [-v]
    cctv-ptz playback [-a ADDRESS] [-v]
    cctv-ptz -h
    cctv-ptz -V

Options:
    -a, --address ADDRESS    - Pelco-D address 0-256. (default = 0)
    -j, --joystick JOYSTICK  - use joystick NUM (e.g. /dev/input/jsNUM). (default = 0)
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

func interactive(conf config.Config) {
	var (
		record        *os.File
		tty           *serial.Port
		err           error
		serialEnabled = ("/dev/null" != conf.SerialPort)
	)

	stdinObserver := listenFile(os.Stdin)

	js, err := joystick.Open(conf.JoystickNumber)
	if err != nil {
		panic(err)
	}
	defer js.Close()

	fmt.Fprintf(os.Stderr, "Joystick port opened. /dev/input/js%d\n", conf.JoystickNumber)
	fmt.Fprintf(os.Stderr, "  Joystick Name: %s\n", js.Name())
	fmt.Fprintf(os.Stderr, "     Axis Count: %d\n", js.AxisCount())
	fmt.Fprintf(os.Stderr, "   Button Count: %d\n", js.ButtonCount())

	jsTicker := time.NewTicker(100 * time.Millisecond)
	jsObserver := listenJoystick(js, jsTicker)

	if serialEnabled {
		ttyOptions := serial.Options{
			Mode:        serial.MODE_WRITE,
			BitRate:     9600,
			DataBits:    8,
			StopBits:    1,
			Parity:      serial.PARITY_NONE,
			FlowControl: serial.FLOWCONTROL_NONE,
		}

		tty, err = ttyOptions.Open(conf.SerialPort)
		if err != nil {
			panic(err)
		}
		defer tty.Close()

		// print serial port info
		func() {
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
		}()
	} else {
		fmt.Fprintf(os.Stderr, "Serial port disabled\n")
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
			if isPressed(state, xbox.X) {
				limitChange(allowAddressChange, func() { conf.Address -= 1 })
			} else if isPressed(state, xbox.Y) {
				limitChange(allowAddressChange, func() { conf.Address += 1 })
			}

			// reset the clock if user presses Back
			if isPressed(state, xbox.Back) {
				startTime = time.Now()
			}

			message := pelcoCreate()
			message = pelcoTo(message, conf.Address)
			message = joystickToPelco(message, state)
			message = pelcoChecksum(message)

			if lastMessage != message {
				millis := (time.Now().Sub(startTime)).Nanoseconds() / 1E6

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

func joystickToPelco(buffer PelcoDMessage, state joystick.State) PelcoDMessage {
	var zoom float32

	panX := normalizeAxis(state, xbox.LeftAxisH)
	panY := normalizeAxis(state, xbox.RightAxisV)
	openIris := isPressed(state, xbox.A)
	closeIris := isPressed(state, xbox.B)
	openMenu := isPressed(state, xbox.Start)

	if isPressed(state, xbox.LeftBumper) {
		zoom = -1.0
	} else if isPressed(state, xbox.RightBumper) {
		zoom = 1.0
	}

	buffer = pelcoApplyJoystick(buffer, panX, panY, zoom, openIris, closeIris, openMenu)

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
			time.Sleep(100 * time.Millisecond)
		}
	}()

	return io
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

func pelcoApplyJoystick(buffer PelcoDMessage, panX, panY, zoom float32, openIris, closeIris, openMenu bool) PelcoDMessage {
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
	buffer[DATA_1] = uint8(float64(0x3F) * math.Abs(float64(panX)))

	if panY > 0 {
		buffer[COMMAND_2] |= 1 << 3
	} else if panY < 0 {
		buffer[COMMAND_2] |= 1 << 4
	}

	// tilt speed
	buffer[DATA_2] = uint8(float64(0x3F) * math.Abs(float64(panY)))

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
	stdinObserver := listenFile(os.Stdin)

	for {
		select {
		case bytes := <-stdinObserver:
			fmt.Printf("%s\n", bytes)
			return
		}
	}
}

func version() string {
	return fmt.Sprintf("%s: version %s, build %s\n\n", os.Args[0], VERSION, BUILD_DATE)
}
