package btrd

import (
	"errors"
	"log"
	"math"
	"sync"

	"github.com/Knetic/govaluate"
	"github.com/tarm/serial"
)

// Adc is Analog to Digital Converter item.
// Vref - Volatge Reference
// Expr - String expression for calculation Adc.value
// It's function f(adcval, vref), where adcval will get 8bit level value (0-255)
// from ADC and vref will be replaced with Adc.Vref.
// For example, Adc.Expr = "adcval * (vref / 256)"
// Cmdget is communication comand for getting the measurement result from ADC.
type Adc struct {
	ID     string `toml:"id"`
	value  float64
	Vref   float64 `toml:"vref"`
	Cmdget string  `toml:"cmdget"`
	Expr   string  `toml:"expr"`
	*Btdev
}

// Tmpt is temperature sensor item (ds18b20 sensor)
// Cmdlsb and Cmdmsb are communication comands for getting the least significant bits (LSB)
// and most significant bits (MSB) of result from sensor.
type Tmpt struct {
	ID     string `toml:"id"`
	value  float64
	Cmdlsb string `toml:"cmdlsb"`
	Cmdmsb string `toml:"cmdmsb"`
	*Btdev
}

// Swt is two-state switch item.
// Cmdget and Cmdset are communication comands for getting and setting state of switch.
// Cmdclr is communication comand for clearing state of switch.
type Swt struct {
	ID     string `toml:"id"`
	value  int
	Cmdget string `toml:"cmdget"`
	Cmdset string `toml:"cmdset"`
	Cmdclr string `toml:"cmdclr"`
	*Btdev
}

type value interface {
}

func getFloat(unkn interface{}) (float64, error) {
	switch i := unkn.(type) {
	case float64:
		return i, nil
	case float32:
		return float64(i), nil
	case int:
		return float64(i), nil
	default:
		return math.NaN(), errors.New("getFloat: unknown value is incompatible type")
	}
}

// ConvertTemp function for converting result from ds18b20 temperature sensor
func ConvertTemp(msb byte, lsb byte) float64 {
	tsign := msb >> 7
	tremain := float64(lsb&15) * 0.0625
	tcom := (msb << 4 & 127) | (lsb >> 4)
	temp := float64(tcom) + tremain
	if tsign == 1 {
		temp = -(128 - temp)
	}
	return temp
}

// ReadValue is method for reading value from ADC to Adc.value
func (a *Adc) ReadValue() error {
	if _, err := a.serport.Write([]byte(a.Cmdget)); err != nil {
		log.Printf("Serial port %s write error", a.Devfile)
		return err
	}
	val := make([]byte, 1)
	if _, err := a.serport.Read(val); err != nil {
		log.Printf("Serial port %s read error", a.Devfile)
		return err
	}

	expr, err := govaluate.NewEvaluableExpression(a.Expr)
	if err != nil {
		log.Fatal(err)
	}
	parameters := make(map[string]interface{}, 8)
	parameters["adcval"] = float64(val[0])
	parameters["vref"] = float64(a.Vref)
	result, err := expr.Evaluate(parameters)
	if err != nil {
		log.Fatal(err)
	}
	a.value, err = getFloat(result)
	if err != nil {
		log.Fatal(err)
	}
	return nil
}

// ReadValue is method for reading value from temperature sensor to Tmpt.value
func (t *Tmpt) ReadValue() error {
	if _, err := t.serport.Write([]byte(t.Cmdlsb)); err != nil {
		log.Printf("Serial port %s write error", t.Devfile)
		return err
	}
	lsb := make([]byte, 1)
	if _, err := t.serport.Read(lsb); err != nil {
		log.Printf("Serial port %s read error", t.Devfile)
		return err
	}
	if _, err := t.serport.Write([]byte(t.Cmdmsb)); err != nil {
		log.Printf("Serial port %s write error", t.Devfile)
		return err
	}
	msb := make([]byte, 1)
	if _, err := t.serport.Read(msb); err != nil {
		log.Printf("Serial port %s read error", t.Devfile)
		return err
	}
	t.value = ConvertTemp(msb[0], lsb[0])
	return nil
}

// ReadValue is method for reading value from switch item to Swt.value
func (sw *Swt) ReadValue() error {
	if _, err := sw.serport.Write([]byte(sw.Cmdget)); err != nil {
		log.Printf("Serial port %s write error", sw.Devfile)
		return err
	}
	res := make([]byte, 1)
	if _, err := sw.serport.Read(res); err != nil {
		log.Printf("Serial port %s read error", sw.Devfile)
		return err
	}
	if (res[0] != 0) && (res[0] != 1) {
		log.Println("Wrong value of switch:", res[0])
		return errors.New("Wrong value of switch")
	}
	sw.value = int(res[0])
	return nil
}

// SetBit method set state of switch item to 1
func (sw *Swt) SetBit() error {
	if _, err := sw.serport.Write([]byte(sw.Cmdset)); err != nil {
		log.Printf("Serial port %s write error", sw.Devfile)
		return err
	}
	res := make([]byte, 1)
	if _, err := sw.serport.Read(res); err != nil {
		log.Printf("Serial port %s read error", sw.Devfile)
		return err
	}
	if res[0] != 'K' {
		log.Printf("Error occurred during setting %s switch bit", sw.ID)
		return errors.New("Error occurred during setting switch bit")
	}
	return nil
}

// ClearBit method clear state of switch item to 0
func (sw *Swt) ClearBit() error {
	if _, err := sw.serport.Write([]byte(sw.Cmdclr)); err != nil {
		log.Printf("Serial port %s write error", sw.Devfile)
		return err
	}
	res := make([]byte, 1)
	if _, err := sw.serport.Read(res); err != nil {
		log.Printf("Serial port %s read error", sw.Devfile)
		return err
	}
	if res[0] != 'K' {
		log.Printf("Error occurred during clearing %s switch bit", sw.ID)
		return errors.New("Error occurred clearing setting switch bit")
	}
	return nil
}

// Btdev is remote device
// Devfile is path to file of serial port
// with certain Baud rate.
type Btdev struct {
	ID           string
	Devfile      string `toml:"devfile"`
	Baud         int    `toml:"baud"`
	serport      *serial.Port
	Adcs         []*Adc  `toml:"adcs"`
	Tmpts        []*Tmpt `toml:"tmpts"`
	Swts         []*Swt  `toml:"swts"`
	serportMutex *sync.Mutex
}

// OpenPort method for opening port of remote device
func (btd *Btdev) OpenPort() {
	c := &serial.Config{Name: btd.Devfile, Baud: btd.Baud}
	serport, err := serial.OpenPort(c)
	if err != nil {
		log.Println("Btdev open serial port problem:", err)
	}
	btd.serport = serport
}

// ClosePort method for opening port of remote device
func (btd *Btdev) ClosePort() {
	btd.serport.Close()
}
