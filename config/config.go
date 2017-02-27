package config

import (
	"github.com/spf13/viper"
)

const MaxSpeed int32 = 0x2f

type Config struct {
	Address        int
	BaudRate       int
	JoystickNumber int
	MaxSpeed       int32
	SerialPort     string
	RecordFile     string
	Verbose        bool
}

var defaultConfig = Config{0, 9600, 0, MaxSpeed, "/dev/ttyUSB0", "/dev/null", false}

func GetDefault() Config {
	return defaultConfig
}

func Load(args map[string]interface{}) Config {
	viper.SetConfigName("cctz-ptz")
	viper.AddConfigPath("./")
	viper.AddConfigPath("/etc/")
	viper.AddConfigPath("$HOME/.config/cctv-ptz/")

	viper.ReadInConfig()

	viper.SetEnvPrefix("cctv")
	viper.AutomaticEnv()

	viper.SetDefault("address", defaultConfig.Address)
	viper.SetDefault("baud", defaultConfig.BaudRate)
	viper.SetDefault("joystick", defaultConfig.JoystickNumber)
	viper.SetDefault("max-speed", defaultConfig.MaxSpeed)
	viper.SetDefault("serial", defaultConfig.SerialPort)
	viper.SetDefault("record", defaultConfig.RecordFile)
	viper.SetDefault("verbose", defaultConfig.Verbose)

	setArg("address", args["--address"])
	setArg("baud", args["--baud"])
	setArg("joystick", args["--joystick"])
	setArg("max-speed", args["--maxspeed"])
	setArg("serial", args["--serial"])
	setArg("record", args["--record"])
	setArg("verbose", args["--verbose"])

	config := Config{}
	config.Address = viper.GetInt("address")
	config.BaudRate = viper.GetInt("baud")
	config.JoystickNumber = viper.GetInt("joystick")
	config.MaxSpeed = int32(viper.GetInt("max-speed")) * MaxSpeed / 100
	config.SerialPort = viper.GetString("serial")
	config.RecordFile = viper.GetString("record")
	config.Verbose = viper.GetBool("verbose")

	return config
}

func setArg(key string, arg interface{}) {
	if nil != arg {
		viper.Set(key, arg)
	}
}
