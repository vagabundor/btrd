package btrd

import (
	"errors"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/Knetic/govaluate"
	"github.com/tarm/serial"
)

// ADC is Analog to Digital Converter item.
// Vref - Volatge Reference
// Expr - String expression for calculation ADC.Value
// It's function f(ADCval, vref), where ADCval will get 8bit level value (0-255)
// from ADC and vref will be replaced with ADC.Vref.
// For example, ADC.Expr = "ADCval * (vref / 256)"
// Cmdget is communication comand for getting the measurement result from ADC.
type ADC struct {
	ID     string  `toml:"id"`
	Vref   float64 `toml:"vref"`
	Cmdget string  `toml:"cmdget"`
	Expr   string  `toml:"expr"`
	*Btdev
	valmux sync.RWMutex
	value  float64
}

// Tmpt is temperature sensor item (ds18b20 sensor)
// Cmdlsb and Cmdmsb are communication comands for getting the least significant bits (LSB)
// and most significant bits (MSB) of result from sensor.
type Tmpt struct {
	ID     string `toml:"id"`
	Cmdlsb string `toml:"cmdlsb"`
	Cmdmsb string `toml:"cmdmsb"`
	*Btdev
	valmux sync.RWMutex
	value  float64
}

// Swt is two-state switch item.
// Cmdget and Cmdset are communication comands for getting and setting state of switch.
// Cmdclr is communication comand for clearing state of switch.
type Swt struct {
	ID     string `toml:"id"`
	Cmdget string `toml:"cmdget"`
	Cmdset string `toml:"cmdset"`
	Cmdclr string `toml:"cmdclr"`
	*Btdev
	valmux sync.RWMutex
	value  int
}

// Value method for getting ADC result
func (a *ADC) Value() float64 {
	a.valmux.RLock()
	defer a.valmux.RUnlock()
	return a.value
}

// Value method for getting Temperature result
func (t *Tmpt) Value() float64 {
	t.valmux.RLock()
	defer t.valmux.RUnlock()
	return t.value
}

// Value method for getting Switch status result
func (sw *Swt) Value() int {
	sw.valmux.RLock()
	defer sw.valmux.RUnlock()
	return sw.value
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

// ReadValue is method for reading value from ADC to ADC.value
func (a *ADC) ReadValue() error {
	a.sermux.Lock()
	defer a.sermux.Unlock()
	if _, err := a.serport.Write([]byte(a.Cmdget)); err != nil {
		err = fmt.Errorf("Serial port %s write error: %s", a.Devfile, err)
		return err
	}
	val := make([]byte, 1)
	if _, err := a.serport.Read(val); err != nil {
		err = fmt.Errorf("Serial port %s read error: %s", a.Devfile, err)
		return err
	}

	expr, err := govaluate.NewEvaluableExpression(a.Expr)
	if err != nil {
		err = fmt.Errorf("Expression parsing error: %s", err)
		return err
	}
	parameters := make(map[string]interface{}, 8)
	parameters["adcval"] = float64(val[0])
	parameters["vref"] = float64(a.Vref)
	result, err := expr.Evaluate(parameters)
	if err != nil {
		err = fmt.Errorf("Expression parsing error: %s", err)
		return err
	}
	a.valmux.Lock()
	defer a.valmux.Unlock()
	a.value, err = getFloat(result)
	if err != nil {
		return err
	}
	return nil
}

// ReadValue is method for reading value from temperature sensor to Tmpt.value
func (t *Tmpt) ReadValue() error {
	t.sermux.Lock()
	defer t.sermux.Unlock()
	if _, err := t.serport.Write([]byte(t.Cmdlsb)); err != nil {
		err = fmt.Errorf("Serial port %s write error: %s", t.Devfile, err)
		return err
	}
	lsb := make([]byte, 1)
	if _, err := t.serport.Read(lsb); err != nil {
		err = fmt.Errorf("Serial port %s read error: %s", t.Devfile, err)
		return err
	}
	if _, err := t.serport.Write([]byte(t.Cmdmsb)); err != nil {
		err = fmt.Errorf("Serial port %s write error: %s", t.Devfile, err)
		return err
	}
	msb := make([]byte, 1)
	if _, err := t.serport.Read(msb); err != nil {
		err = fmt.Errorf("Serial port %s read error: %s", t.Devfile, err)
		return err
	}
	t.valmux.Lock()
	defer t.valmux.Unlock()
	t.value = ConvertTemp(msb[0], lsb[0])
	return nil
}

// ReadValue is method for reading value from switch item to Swt.value
func (sw *Swt) ReadValue() error {
	sw.sermux.Lock()
	defer sw.sermux.Unlock()
	if _, err := sw.serport.Write([]byte(sw.Cmdget)); err != nil {
		err = fmt.Errorf("Serial port %s write error: %s", sw.Devfile, err)
		return err
	}
	res := make([]byte, 1)
	if _, err := sw.serport.Read(res); err != nil {
		err = fmt.Errorf("Serial port %s read error: %s", sw.Devfile, err)
		return err
	}
	if (res[0] != 0) && (res[0] != 1) {
		err := fmt.Errorf("Wrong value of switch %s: %b", sw.ID, res[0])
		return err
	}
	sw.valmux.Lock()
	defer sw.valmux.Unlock()
	sw.value = int(res[0])
	return nil
}

// SetBit method set state of switch item to 1
func (sw *Swt) SetBit() error {
	sw.sermux.Lock()
	defer sw.sermux.Unlock()
	if _, err := sw.serport.Write([]byte(sw.Cmdset)); err != nil {
		err = fmt.Errorf("Serial port %s write error: %s", sw.Devfile, err)
		return err
	}
	res := make([]byte, 1)
	if _, err := sw.serport.Read(res); err != nil {
		err = fmt.Errorf("Serial port %s read error: %s", sw.Devfile, err)
		return err
	}
	if res[0] != 'K' {
		err := fmt.Errorf("Error occurred during setting %s switch bit. Answer is not K.", sw.ID)
		return err
	}
	return nil
}

// ClearBit method clear state of switch item to 0
func (sw *Swt) ClearBit() error {
	sw.sermux.Lock()
	defer sw.sermux.Unlock()
	if _, err := sw.serport.Write([]byte(sw.Cmdclr)); err != nil {
		err = fmt.Errorf("Serial port %s write error: %s", sw.Devfile, err)
		return err
	}
	res := make([]byte, 1)
	if _, err := sw.serport.Read(res); err != nil {
		err = fmt.Errorf("Serial port %s read error: %s", sw.Devfile, err)
		return err
	}
	if res[0] != 'K' {
		err := fmt.Errorf("Error occurred during setting %s switch bit. Answer is not K.", sw.ID)
		return err
	}
	return nil
}

// Btdev is remote device
// Devfile is path to file of serial port
// with certain Baud rate.
type Btdev struct {
	ID      string
	Devfile string  `toml:"devfile"`
	Baud    int     `toml:"baud"`
	ADCs    []*ADC  `toml:"ADCs"`
	Tmpts   []*Tmpt `toml:"tmpts"`
	Swts    []*Swt  `toml:"swts"`
	serport *serial.Port
	sermux  sync.Mutex
}

// OpenPort method for opening port of remote device
func (btd *Btdev) OpenPort() error {
	c := &serial.Config{Name: btd.Devfile, Baud: btd.Baud, ReadTimeout: time.Second * 5}
	serport, err := serial.OpenPort(c)
	if err != nil {
		err = fmt.Errorf("Btdev %s open serial port problem: %s", btd.ID, err)
		return err
	}
	btd.serport = serport
	return nil
}

// ClosePort method for opening port of remote device
func (btd *Btdev) ClosePort() {
	btd.serport.Close()
}
