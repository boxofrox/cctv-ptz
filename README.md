# Description

A small console utility that allows a user to plug a USB Xbox controller and an
RS485-to-USB adapter into their computer and generate Pelco-D compliant
pan-tilt-zoom (PTZ) commands for a closed-circuit-television (CCTV) camera.

# System Requirements

Tested and used on Linux.  Should work on MacOS.  May work on Windows.

Serial port and joystick libraries are supposed to support all platforms.

# Progress

This is an initial implementation.  Not all features are complete.

### Done

- [x] Mapping Xbox controls to Pelco-D commands.
  - [x] pan X/Y
  - [x] zoom in/out
  - [x] iris open/close
  - [x] open menu (default command is set preset 95. manufacturers may differ)
  - [x] Pelco-D address +/-
- [x] Command-line options
  - [x] Set initial Pelco-D address.
  - [x] Set serial port baud rate.
  - [x] Select serial port by name/path.
  - [x] Select joystick by number.
- [x] Record commands to text file.
- [x] Playback commands from stdin.

### Todo

- [ ] Override playback address with command line option.

### Wishlist

- [ ] Refactor controller definitions to support a variety of PC controllers.
- [ ] Customize controller mappings via config file.

# Usage

    CCTV Pan-Tilt-Zoom via Xbox Controller

    Usage:
        cctv-ptz [-v] [-a ADDRESS] [-s FILE] [-j JOYSTICK] [-r FILE] [-b BAUD]
        cctv-ptz playback [-a ADDRESS] [-s FILE] [-b BAUD] [-v]
        cctv-ptz -h
        cctv-ptz -V

    Options:
        -a, --address ADDRESS    - Pelco-D address 0-256. (default = 0)
        -b, --baud BAUD          - set baud rate of serial port. (default = 9600)
        -j, --joystick JOYSTICK  - use joystick NUM (e.g. /dev/input/jsNUM). (default = 0)
        -s, --serial FILE        - assign serial port for rs485 output. (default = /dev/sttyUSB0)
        -r, --record FILE        - record rs485 commands to file. (default = /dev/null)
        -v, --verbose            - prints Pelco-D commands to stdout.
        -h, --help               - print this help message.
        -V, --version            - print version info.

# Default Mapping

    Controller Layout

    Controller                   Command
    ----------                   ------
    Left Analog
      Up                         (unused)
      Down                       (unused)
      Left                       Left
      Right                      Right
    Right Analog
      Up                         Up
      Down                       Down
      Left                       (unused)
      Right                      (unused)
    Directional Pad
      Up                         (unused)
      Down                       (unused)
      Left                       (unused)
      Right                      (unused)
    A                            Iris Open
    B                            Iris Close
    X                            Decrement Address
    Y                            Increment Address
    Left Bumper                  Zoom Out
    Right Bumper                 Zoom In
    Start                        Menu (Go to Preset 95)
    Back                         Reset recording start time
    Left Trigger                 (unused)
    Right Trigger                (unused)

# Hacking

### Changing default mapping

Find near the top of `main.go` the `ptz struct`.  Modify the initialization
values to map the controller inputs to commands.  The `xbox struct` defines
names for the controller inputs.

### Playback notes

The Pelco-D protocol effectively limits playback to a dead-reckoning system.
Small variations in timing or camera speed will amplify into large errors over
time.  YMMV.
