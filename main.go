package main

import (
	"bufio"
	"fmt"
	"github.com/simulatedsimian/joystick"
	"io"
	"os"
	"time"
)

var (
	VERSION    string
	BUILD_DATE string
)

const AxisMax = 32767

type Axis struct {
	Index    int
	Min      int // used for normalizing input -1.0 to 1.0
	Max      int
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
	LeftBumper   int
	RightBumper  int
	A            int
	B            int
	X            int
	Y            int
	Start        int
	Back         int
	XBox         int
}{Axis{0, -AxisMax, AxisMax, false}, // left axis
	Axis{1, -AxisMax, AxisMax, true},
	Axis{3, -AxisMax, AxisMax, false}, // right axis
	Axis{4, -AxisMax, AxisMax, true},
	Axis{2, -AxisMax, AxisMax, false}, // triggers
	Axis{5, -AxisMax, AxisMax, false},
	Axis{6, -AxisMax, AxisMax, false}, // dpad
	Axis{7, -AxisMax, AxisMax, false},
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

func listenFile(f io.Reader) <-chan []byte {
	io := make(chan []byte)
	scanner := bufio.NewScanner(f)

	go func() {
		for scanner.Scan() {
			io <- scanner.Bytes()
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

func main() {
	js, err := joystick.Open(0)
	if err != nil {
		panic(err)
	}

	defer js.Close()

	fmt.Printf("Joystick Name: %s\n", js.Name())
	fmt.Printf("   Axis Count: %d\n", js.AxisCount())
	fmt.Printf(" Button Count: %d\n", js.ButtonCount())

	jsTicker := time.NewTicker(100 * time.Millisecond)
	jsObserver := listenJoystick(js, jsTicker)

	stdinObserver := listenFile(os.Stdin)

	for {
		select {
		case state := <-jsObserver:
			fmt.Printf("\033[KJoystick Data: %v\t%v\r", state.AxisData, state.Buttons)
		case <-stdinObserver:
			break
		}
	}

}
