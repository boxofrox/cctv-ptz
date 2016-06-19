package config

import (
	"github.com/spf13/viper"
)

type Config struct {
	Address        int
	BaudRate       int
	JoystickNumber int
	SerialPort     string
	RecordFile     string
	Verbose        bool
}

var defaultConfig = Config{0, 9600, 0, "/dev/ttyUSB0", "/dev/null", false}

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
	viper.SetDefault("serial", defaultConfig.SerialPort)
	viper.SetDefault("record", defaultConfig.RecordFile)
	viper.SetDefault("verbose", defaultConfig.Verbose)

	setArg("address", args["--address"])
	setArg("baud", args["--baud"])
	setArg("joystick", args["--joystick"])
	setArg("serial", args["--serial"])
	setArg("record", args["--record"])
	setArg("verbose", args["--verbose"])

	config := Config{}
	config.Address = viper.GetInt("address")
	config.BaudRate = viper.GetInt("baud")
	config.JoystickNumber = viper.GetInt("joystick")
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
