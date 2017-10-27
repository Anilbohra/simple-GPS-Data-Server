// utils to filter message from GPS device received from TCP port and create openGTS HTTP request (GPRMC record)
// =============================================================================================================
// input: raw tcp string
// output: response for TCP-client, query string for HTTP request, error code
// 
package main

import (
        "fmt"
		"regexp"
		"errors"
		"strconv"
		"math"
		"net/url"
		"os"
		"bufio"
		"strings"
		"encoding/json"
)

const (	
	NONE 	int = iota
	DEVID	int = iota
	DEVIMEI	int = iota
	GPRMC 	int = iota	
	TIME	int = iota
	ACTIVE	int = iota
	LAT		int = iota
	LON		int = iota
	NS		int = iota
	EW		int = iota
	SPEED	int = iota
	ANGLE	int = iota
	DATE	int = iota
	ALT		int = iota
	ACC		int = iota
	DEGMIN	int = iota
	KMPERH 	int = iota
	MPERS 	int = iota
	KNOTS	int = iota
	DEGREE 	int = iota
	HEAD	int = iota
	CHECK 	int = iota
	MAGN 	int = iota
)

var keywords = map[string]int{
    "NONE": 	NONE,
    "DEVID":   	DEVID,
	"DEVIMEI":	DEVIMEI,
	"GPRMC": 	GPRMC,	
	"TIME":		TIME,
	"ACTIVE":	ACTIVE,
	"LAT":		LAT,
	"LON":		LON,
	"NS":		NS,
	"EW":		EW,
	"SPEED":	SPEED,
	"ANGLE":	ANGLE,
	"DATE":		DATE,
	"ALT":		ALT,
	"ACC":		ACC,
	"DEGMIN":	DEGMIN,
	"KMPERH": 	KMPERH,
	"MPERS": 	MPERS,
	"KNOTS":	KNOTS,
	"DEGREE": 	DEGREE,
	"HEAD":		HEAD,
	"CHECK": 	CHECK,
	"MAGN": 	MAGN,
}

type keys struct {
	key string
	
}

type ReqRespPat struct { // regular expressions describing the message + response
	Msg 	string
	Resp 	string
	MsgRegexp *regexp.Regexp
}

type devPattern struct {
	Device 		string		// device name/imei 
	Login 		ReqRespPat
	Heartbeat	ReqRespPat
	Gps_data	ReqRespPat 
	Order		[]int		// define the Order of the incoming parameters. List in above GPSDATA enum
	Units		[]int		// for unit conversion provide unit of parameter (enum UNITS)
} 

// 
// regular expression for GPRMC record w/o header, magnetic deviation and checksum 
const (
	REGEXP_GPRMC = "([0-9]{6},[A|V]*,[0-9.]+,[N|S],[0-9.]+,[E|W],[0-9.]+,[0-9.]+,[0-9]{6})"
//                    time  active/void  lat          lon          speed   angle    date
)

var devs []devPattern

// how to define device patterns:
// - regexp pattern required for login, heartbeat and actual data message
// - for each case a response can be defined. Currently NO dynamic response possible
// - in each case the device has to be identified by the IMEI or a deviceid
// - for data message
// 	o if device sends a GPRMC record, use the predefined constant (see above)
// 	o assign to each matched pattern (in parentheses) a key word (DEVIMEI, DEVID, ACTIVE, LAT, LON, NS, EW, SPEED, ANGLE, DATE)
// 	o for unit conversion give for each matched pattern (LAT, LON, SPEED, ANGLE) the unit 

//example Heartbeat: *HQ,355488020824039,XT,V,0,0#
//                               IMEI
//Heartbeat: ReqRespPat{Msg:"^\\*\\w{2},(\\d{15}),XT,[V|A]*,([0-9]+),([0-9]+)#\\s*$", Resp:""},

//example data: *HQ,355488020824039,V1,114839,A,   5123.85516,N,  00703.64046,E,  0.03,  0,    010917,EFE7FBFF#
//                  imei               time   A/V  lat        N/S long        E/W speed  angle date   Status bits
//Gps_data: ReqRespPat{Msg:"^\\*\\w{2},([0-9]{15}),V1,([0-9]{6}),([A|V]*),([0-9.]+),([N|S]),([0-9.]+),([E|W]),([0-9.]+),([0-9.]+),([0-9]{6}),(\\w+)#\\s*$", Resp:""},
//Order: []int{DEVIMEI,TIME,ACTIVE,LAT,NS,LON,EW,SPEED,ANGLE,DATE},
//Units: []int{NONE,NONE,NONE,DEGMIN,NONE,DEGMIN,NONE,KMPERH,DEGREE,NONE,NONE},
 
var devices = []devPattern {
// ------------  TK103_H02
		devPattern {Device:"TK103B-H02",
			Login: 		ReqRespPat{Msg:"", 															Resp:"",MsgRegexp:nil},
			Heartbeat: 	ReqRespPat{Msg:"^\\*\\w{2},(\\d{15}),XT,[V|A]*,([0-9]+),([0-9]+)#\\s*$", 	Resp:""},
			Gps_data: 	ReqRespPat{Msg:"^\\*\\w{2},([0-9]{15}),V1,"+REGEXP_GPRMC+",.*$", 			Resp:""},
			Order: []int{                         DEVIMEI,           GPRMC},
			Units: []int{NONE,NONE},
		},
// ------------ GPS-logger via UDP
		devPattern {Device:"GPS Logger (UDP)",
			Login: ReqRespPat{Msg:"", 																Resp:"",MsgRegexp:nil},
			Heartbeat: ReqRespPat{Msg:"", 															Resp:""},
			//example data: s08754/s08754/$GPRMC,180725,A,5337.37477,N,1010.26495,E,0.000000,0.000000,021017,,*20
			//              user   devid       GPRMC record    
			Gps_data: ReqRespPat{Msg:"^\\w+\\/(\\w+)\\/\\$GPRMC,"+REGEXP_GPRMC+",.*$", 				Resp:""},
			Order: []int{					  DEVID,				GPRMC},
			Units: []int{NONE,NONE},
		},
	}
// ------------ TK103
//	{.Device="TK103-untested and incomplete", .type=TK103,
//	 .Login 	= {.Msg="^\\((\\d{12})(BP05)([A-Z0-9.]*)\\).*$", .Resp="%DATE%%TIME%AP05HSO"},
//	 .Heartbeat = {.Msg=NULL, .Resp=NULL},
//	 .Gps_data	= {.Msg="^\\((\\d{12})(B[A-Z]\\d{2})([A-Z0-9.]*)\\).*$", .Resp=NULL},
//	 .Order		= {DEVID},
//	 .Units		= {NONE}},

// ----------- GL103
//	{.Device="GL103-untested and incomplete", .type=GL103,
//	 .Login 	= {.Msg="^##,imei:(\\d+),A;.*$", .Resp="LOAD"},
//	 .Heartbeat = {.Msg="^imei:(\\d+);.*$", .Resp="ON"},
//	 .Gps_data	= {.Msg="^imei:(\\d+),(\\d+|A),?(\\d*),?(\\d+),?([a-z0-9,%.]+);.*$", .Resp=NULL},
//	 .Order     = {DEVID},
//	 .Units		= {NONE}}
//};


func readDeviceConfig(fileconf string) (err error) {
	fconf, err := os.Open(fileconf)
	if err != nil {return}
	defer fconf.Close()
    scanner := bufio.NewScanner(fconf)
	jsonBlob := ""
// remove comment lines
    for scanner.Scan() {
        line := strings.TrimSpace(scanner.Text())
		if len(line)> 0 && line[:1] != "/" { jsonBlob += scanner.Text() }
    }
// replace keywords
	jsonBlob = strings.Replace(jsonBlob,"%REGEXP_GPRMC%",REGEXP_GPRMC,-1)
	for key, idx := range keywords {
		jsonBlob = strings.Replace(jsonBlob,"%"+key+"%",strconv.Itoa(idx),-1)
	}
// find remaining keywords
	re := regexp.MustCompile("\\%\\w+\\%")
	byMatch := re.Find([]byte(jsonBlob))
	if byMatch != nil { fmt.Printf("Unknown key %s found \n",string(byMatch)); return }
//	fmt.Print(jsonBlob)
	var Devices []devPattern
	err = json.Unmarshal([]byte(jsonBlob),&Devices)
	if err != nil {	fmt.Println(err.Error()); return }
//	strjson,_ := json.Marshal(Devices)
//	fmt.Println(string(strjson))
// remove Dummy device at the end of the list
	if Devices[len(Devices)-1].Device == "" { Devices = Devices[:len(Devices)-1] }  
// list found devices and check regexp of msg
	logger.Printf("Found %d device configurations",len(Devices))
	for _,dev := range Devices {
		if dev.Login.Msg != "" { 
			dev.Login.MsgRegexp = regexp.MustCompile(dev.Login.Msg) 
			if dev.Login.MsgRegexp == nil { fmt.Printf("error in regexp of Login: %s \n",dev.Login.Msg); return }
		}
		if dev.Heartbeat.Msg != "" { 
			dev.Heartbeat.MsgRegexp = regexp.MustCompile(dev.Heartbeat.Msg) 
			if dev.Heartbeat.MsgRegexp == nil { fmt.Printf("error in regexp of Heartbeat: %s \n",dev.Heartbeat.Msg); return }
		}
		if dev.Gps_data.Msg != "" { 
			dev.Gps_data.MsgRegexp = regexp.MustCompile(dev.Gps_data.Msg) 
			if dev.Gps_data.MsgRegexp == nil { fmt.Printf("error in regexp of Gps_data: %s \n",dev.Gps_data.Msg); return }
		}
		logger.Printf("Device %s - OK",dev.Device)
	}
	return
}

func filter_gps_device(msg string) (response string, query string, err error) {
	response = ""
	query = ""
	err = nil

	// try to match msg to one of the knows devices
	id := 0
	isLogin := false
	isHeart := false
	isData := false
	var matchedStrings []string
	for i:=0; i<len(devs); i++ {
		dev := devs[i];
		id = i;
		nmatch := 0
		if len(dev.Login.Msg)>0	{
			if dev.Login.MsgRegexp == nil { dev.Login.MsgRegexp = regexp.MustCompile(dev.Login.Msg) }
			matchedStrings = dev.Login.MsgRegexp.FindStringSubmatch(msg)
			if nmatch=len(matchedStrings); nmatch > 2 {
				isLogin = true
				response = dev.Login.Resp
				break
			}
		}
		if len(dev.Heartbeat.Msg)>0	{
			if dev.Heartbeat.MsgRegexp == nil { dev.Heartbeat.MsgRegexp = regexp.MustCompile(dev.Heartbeat.Msg) }
			matchedStrings = dev.Heartbeat.MsgRegexp.FindStringSubmatch(msg)
			if nmatch=len(matchedStrings); nmatch > 2 {
				isHeart = true
				response = dev.Heartbeat.Resp
				break
			}
		}
		if len(dev.Gps_data.Msg)>0	{
			if dev.Gps_data.MsgRegexp == nil { dev.Gps_data.MsgRegexp = regexp.MustCompile(dev.Gps_data.Msg) }
			matchedStrings = dev.Gps_data.MsgRegexp.FindStringSubmatch(msg)
			if nmatch=len(matchedStrings); nmatch > 2 {
				isData = true
				response = dev.Gps_data.Resp
				break
			}
		}
	}
//	fmt.Println(matchedStrings)
	if isLogin {
		logger.Print("Login message of "+devs[id].Device) 
	} else if isHeart {
		logger.Print("Heartbeat message of "+devs[id].Device) 
	} else if isData {
		logger.Print("GPS-data of "+devs[id].Device) 
		if isData { query,err = createGPRMCQuery(devs[id],matchedStrings) }
	} else { 
		err = errors.New("Unknown Device")
		if isVerbose { logger.Print("Unknown Device") } 
	}
	return
} 

// GPRMC format digested by openGTS server
var gprmcOrder = []int{HEAD,TIME,ACTIVE,LAT,NS,LON,EW,SPEED,ANGLE,DATE,MAGN}

func createGPRMCQuery(dev devPattern, matches []string) (query string, err error) {
	err = nil
	query = ""
	val := ""
	if len(matches) < 2 { return }
	// check, if GPRMC record already included in data string
	isGPRMC := false
	for i:=0; !isGPRMC && i<len(dev.Order); i++ { isGPRMC=dev.Order[i]==GPRMC }
	if isGPRMC {
		if val,_=getGPSValue(dev,matches,GPRMC); len(val)>0 { query += "$GPRMC,"+val+",0.0,W" }	// add header and dummy magn deviation
	} else {
		for i:=0; i<len(gprmcOrder);i++ {
			switch gprmcOrder[i] {
					case HEAD:
						query +="$GPRMC"
					case MAGN:	// add dummy magnetic deviation
						query +=",0.0,W";					
					default:
						query += ","
						if val,_=getGPSValue(dev,matches,gprmcOrder[i]); len(val)>0 { query += url.QueryEscape(val) }
			}
		}
	}
	if len(query)>0 {
		// add trailing ACTIVE (NMEA 2.1)
		query += ",A*"
		// calculate single byte GPRMC checksum between $ and *
		var cs byte=0
		for _,c := range []byte(strconv.QuoteToASCII(query)) { if c!='$' && c!='*' { cs ^= c }}
		query = query+fmt.Sprintf("%02X",cs)
		query = "gprmc="+query	// openGTS HTTP request
		if val,_=getGPSValue(dev,matches,DEVID); len(val)>0 	{ query = "id="+url.QueryEscape(val)+"&"+query }
		if val,_=getGPSValue(dev,matches,DEVIMEI); len(val)>0 	{ query = "imei="+url.QueryEscape(val)+"&"+query }
		if val,_=getGPSValue(dev,matches,  ALT); len(val)>0 	{ query = "alt="+url.QueryEscape(val)+"&"+query }
		if val,_=getGPSValue(dev,matches,  ACC); len(val)>0 	{ query = "acc="+url.QueryEscape(val)+"&"+query }		
	}
//	fmt.Println("GPRMC-record : "+query)
	return
}


func getGPSValue(dev devPattern, matches []string, key int) (val string, idx int) {
	i:=0
	val = ""
	for i=0; i<len(dev.Order) && dev.Order[i]!=key;i++ {}
	if i<len(dev.Order) && dev.Order[i]==key && len(matches)>(i+1) { 
		val = matches[i+1] 
		switch key {
			case TIME:	fallthrough
			case DATE:
				val = fmt.Sprintf("%6s",val)
			case ACTIVE:
				
			case LAT:	fallthrough
			case LON:
				if dev.Units[i] == DEGMIN { break }	// correct unit for GPRMC -> do nothing
				degval,err:=strconv.ParseFloat(val,32)
				if err != nil {break}
				if dev.Units[i] == DEGREE {		// calculate degree*100 + minutes
					deg := float64(int(degval))		
					min := (degval - deg)*60.0;
					degfmt := "%02d%05.2f"
					if key == LON { degfmt = "%03d%05.2f" }
					val = fmt.Sprintf(degfmt,math.Abs(deg),min)
				}
			case SPEED:	// get value in m/s (GPRMC stores KNOTS, openGTS expects m/s)
				v,err:=strconv.ParseFloat(val,32)
				if err != nil {break}
				if dev.Units[i] == KMPERH 	{ v /= 1.852 }		// calc knots
				if dev.Units[i] == MPERS 	{ v *= 3.6/1.852 }	// calc knots
				if dev.Units[i] == KNOTS 	{ }					// nothing to do	
				val = fmt.Sprintf("%.1f",v)
			default:
		}
	} else {	// key not in input string -> use default, or determine from different source
		switch key {
			case NS:
				val = "N"
				_,idx = getGPSValue(dev,matches,LAT)	// check sign of lattitude value
				if idx > 0 && idx < len(matches) { 
					degval,err:=strconv.ParseFloat(matches[idx],32)
					if err == nil && degval < 0.0 { val = "S" }
				}
			case EW:
				val = "E"
				_,idx = getGPSValue(dev,matches,LON)	// check sign of longitude value
				if idx > 0 && idx < len(matches) { 
					degval,err:=strconv.ParseFloat(matches[idx],32)
					if err == nil && degval < 0.0 { val = "W" }
				}				
			case ACTIVE:
				val = "A"
			case DEVIMEI:	fallthrough
			case DEVID:
				val = ""
			default:
				val = "0.0"	// default for non-existing keys
		}
	}	
	return 
}

// expected response from web server: device-ID/IMEI OK|REJECTED
var regexpHTTPResponse = regexp.MustCompile("^\\s*[0-9A-Za-z]+\\s+(OK|REJECTED)\\s*")

func analyseHTTPResponse(response string) (ans string, err error) {
	ans = "no valid response - check connection to HTTP server"
	err = errors.New(ans)
	if response != "" {
		matchedStrings := regexpHTTPResponse.FindStringSubmatch(response)
		if nmatch:=len(matchedStrings); nmatch > 1 {
			ans = "device "+matchedStrings[1]
			if isVerbose { logger.Print(ans) } 
			if matchedStrings[1] == "OK" { err = nil } 
		}
	}
	return
}
