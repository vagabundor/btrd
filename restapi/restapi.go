package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/julienschmidt/httprouter"
	"github.com/vagabundor/btrd"
)

const failtimeout time.Duration = 30 * time.Second
const maxerrors int = 3
const errpause time.Duration = 4 * time.Second

// LoadConfig method decodes config from toml
func loadConfig(confstr string) map[string]*btrd.Btdev {

	var config map[string]*btrd.Btdev
	_, err := toml.Decode(confstr, &config)
	if err != nil {
		log.Fatal(err)
	}

	for btk, btv := range config {
		btv.ID = btk
		switch {
		case btv.Baud == 0:
			log.Fatalf("Baud rate of device <%s> is not defined \n", btk)
		case btv.Devfile == "":
			log.Fatalf("Device file of <%s> is not defined \n", btk)
		}

		for _, adcv := range btv.ADCs {
			adcv.Btdev = btv
			switch {
			case adcv.ID == "":
				log.Fatalf("ID of adc in <%s> is not defined \n", btk)
			case adcv.Cmdget == "":
				log.Fatalf("Cmdget of adc <%s> in <%s> is not defined \n", adcv.ID, btk)
			}
		}

		for _, tmptv := range btv.Tmpts {
			tmptv.Btdev = btv
			switch {
			case tmptv.ID == "":
				log.Fatalf("ID of tmpt in <%s> is not defined \n", btk)
			case tmptv.Cmdlsb == "":
				log.Fatalf("Cmdlsb of tmpt <%s> in <%s> is not defined \n", tmptv.ID, btk)
			case tmptv.Cmdmsb == "":
				log.Fatalf("Cmdmsb of tmpt <%s> in <%s> is not defined \n", tmptv.ID, btk)
			}
		}

		for _, swtv := range btv.Swts {
			swtv.Btdev = btv
			switch {
			case swtv.ID == "":
				log.Fatalf("ID of swt in <%s> is not defined \n", btk)
			case swtv.Cmdget == "":
				log.Fatalf("Cmdget of swt <%s> in <%s> is not defined \n", swtv.ID, btk)
			case swtv.Cmdset == "":
				log.Fatalf("Cmdset of swt <%s> in <%s> is not defined \n", swtv.ID, btk)
			case swtv.Cmdclr == "":
				log.Fatalf("Cmdclr of swt <%s> in <%s> is not defined \n", swtv.ID, btk)
			}
		}
	}
	return config
}

func readHandler(w http.ResponseWriter, r *http.Request, ps httprouter.Params, bt map[string]*btrd.Btdev) {
	if btd, ok := bt[ps.ByName("btdevID")]; ok {
		switch ps.ByName("itemtypes") {
		case "adcs":
			for _, itv := range btd.ADCs {
				if ps.ByName("itemID") == itv.ID {
					fmt.Fprintf(w, "%.2f\n", itv.Value())
					return
				}
			}
		case "tmpts":
			for _, itv := range btd.Tmpts {
				if ps.ByName("itemID") == itv.ID {
					fmt.Fprintf(w, "%3.1f\n", itv.Value())
					return
				}
			}
		case "swts":
			for _, itv := range btd.Swts {
				if ps.ByName("itemID") == itv.ID {
					switch itv.Value() {
					case 1:
						fmt.Fprint(w, "true\n")
						return
					case 0:
						fmt.Fprint(w, "false\n")
						return
					}
				}
			}
		}
	}
	w.WriteHeader(400)
}

func changeHandler(w http.ResponseWriter, r *http.Request, ps httprouter.Params, bt map[string]*btrd.Btdev) {
	if btd, ok := bt[ps.ByName("btdevID")]; ok {
		if ps.ByName("itemtypes") == "swts" {
			body, err := ioutil.ReadAll(r.Body)
			if err != nil {
				log.Println(err)
				w.WriteHeader(500)
				return
			}
			state := string(body)
			if state != "true" && state != "false" {
				w.WriteHeader(400)
				return
			}
			for _, itv := range btd.Swts {
				if ps.ByName("itemID") == itv.ID {
					switch state {
					case "true":
						if err := itv.SetBit(); err != nil {
							log.Println(err)
							w.WriteHeader(500)
							return
						}
					case "false":
						if err := itv.ClearBit(); err != nil {
							log.Println(err)
							w.WriteHeader(500)
							return
						}
					default:
						w.WriteHeader(400)
						return
					}
					w.WriteHeader(200)
					return
				}
			}
		}
	}
	w.WriteHeader(400)
}

func main() {
	var bindaddr string
	var confile string
	flag.StringVar(&bindaddr, "bind", "127.0.0.1:5500", "a server bind address")
	flag.StringVar(&confile, "conf", "config.toml", "config file path")
	flag.Parse()

	file, err := os.Open(confile)
	defer file.Close()
	if err != nil {
		flag.Usage()
		log.Fatal("error:", err)
	}
	b, err := ioutil.ReadFile(confile)
	if err != nil {
		log.Fatal("error:", err)
	}
	bt := loadConfig(string(b))
	log.Println("API server launched")
	for _, btv := range bt {
		go func(bt *btrd.Btdev) {
			log.Printf("Polling routine for device <%s> started..", bt.ID)
			if err := bt.OpenPort(); err != nil {
				log.Println(err)
			}
			defer bt.ClosePort()
			var errcounter int
			for {
				for _, adc := range bt.ADCs {
					if err := adc.ReadValue(); err != nil {
						time.Sleep(errpause)
						errcounter++
					} else {
						errcounter = 0
					}
				}
				for _, tp := range bt.Tmpts {
					if err := tp.ReadValue(); err != nil {
						log.Println(err)
						time.Sleep(errpause)
						errcounter++
					} else {
						errcounter = 0
					}
				}
				for _, sw := range bt.Swts {
					if err := sw.ReadValue(); err != nil {
						log.Println(err)
						time.Sleep(errpause)
						errcounter++
					} else {
						errcounter = 0
					}
				}
				if errcounter > maxerrors {
					bt.ClosePort()
					log.Printf("Pause for %s %.0f seconds", bt.ID, failtimeout.Seconds())
					time.Sleep(failtimeout)
					if err := bt.OpenPort(); err != nil {
						log.Println(err)
					}
					defer bt.ClosePort()
					log.Printf("Port %s reopening.", bt.Devfile)
					errcounter = 0
				}
			}
		}(btv)
	}
	router := httprouter.New()
	router.GET("/:btdevID/:itemtypes/:itemID", func(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
		readHandler(w, r, ps, bt)
	})
	router.POST("/:btdevID/:itemtypes/:itemID", func(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
		changeHandler(w, r, ps, bt)
	})
	log.Println("Server listening on", bindaddr)
	log.Fatal(http.ListenAndServe(bindaddr, router))
}
