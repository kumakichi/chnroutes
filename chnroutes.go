package main

import (
	"bufio"
	"encoding/binary"
	"flag"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"os"
	"regexp"
	"strconv"
)

type apnicData struct {
	startIp string
	mask    net.IP
	maskNum int
}

var (
	platform string
	metric   int
)

func init() {
	flag.StringVar(&platform, "p", "openvpn", "Target platforms, it can be openvpn, mac, linux,win, android. openvpn by default.")
	flag.IntVar(&metric, "m", 5, "Metric setting for the route rules")
}

func main() {
	router := map[string]func([]apnicData){
		"openvpn": generate_open,
		"linux":   generate_linux,
		"mac":     generate_mac,
		"win":     generate_win,
		"android": generate_android,
	}

	flag.Parse()
	if fun := router[platform]; fun != nil {
		data := fetch_ip_data()
		fun(data)
	} else {
		fmt.Printf("Platform %s is not supported.\n", platform)
	}
}

func generate_open(data []apnicData) {
	fp := safeCreateFile("routes.txt")
	defer fp.Close()

	for _, v := range data {
		route_item := fmt.Sprintf("route %s %s net_gateway %d\n", v.startIp, v.mask.String(), metric)
		fp.WriteString(route_item)
	}

	fmt.Printf("Usage: Append the content of the newly created routes.txt to your openvpn config file, and also add 'max-routes %d', which takes a line, to the head of the file.\n", len(data)+20)
}

func generate_linux(data []apnicData) {
	upfile := safeCreateFile("ip-pre-up")
	downfile := safeCreateFile("ip-down")
	defer upfile.Close()
	defer downfile.Close()

	upfile.WriteString(linux_upscript_header)
	downfile.WriteString(linux_downscript_header)

	for _, v := range data {
		upstr := fmt.Sprintf("route add -net %s netmask %s gw $OLDGW\n", v.startIp, v.mask.String())
		upfile.WriteString(upstr)
		dnstr := fmt.Sprintf("route del -net %s netmask %s\n", v.startIp, v.mask.String())
		downfile.WriteString(dnstr)
	}
	downfile.WriteString("rm /tmp/vpn_oldgw\n")

	fmt.Println("For pptp only, please copy the file ip-pre-up to the folder/etc/ppp, please copy the file ip-down to the folder /etc/ppp/ip-down.d.")
}

func generate_mac(data []apnicData) {
	upfile := safeCreateFile("ip-up")
	downfile := safeCreateFile("ip-down")
	defer upfile.Close()
	defer downfile.Close()

	upfile.WriteString(mac_upscript_header)
	downfile.WriteString(mac_downscript_header)

	for _, v := range data {
		upstr := fmt.Sprintf("route add %s/%d \"${OLDGW}\"\n", v.startIp, v.maskNum)
		upfile.WriteString(upstr)
		dnstr := fmt.Sprintf("route delete %s/%d ${OLDGW}\n", v.startIp, v.maskNum)
		downfile.WriteString(dnstr)
	}
	downfile.WriteString("\n\nrm /tmp/pptp_oldgw\n")

	fmt.Println("For pptp on mac only, please copy ip-up and ip-down to the /etc/ppp folder, don't forget to make them executable with the chmod command.")
}

func generate_win(data []apnicData) {
	upfile := safeCreateFile("vpnup.bat")
	downfile := safeCreateFile("vpndown.bat")
	defer upfile.Close()
	defer downfile.Close()

	upfile.WriteString(ms_upscript_header)
	upfile.WriteString("ipconfig /flushdns\n\n")
	downfile.WriteString("@echo off\n")

	for _, v := range data {
		upstr := fmt.Sprintf("route add %s mask %s %%gw%% metric %d\n", v.startIp, v.mask.String(), metric)
		upfile.WriteString(upstr)
		dnstr := fmt.Sprintf("route delete %s\n", v.startIp)
		downfile.WriteString(dnstr)
	}

	fmt.Println("For pptp on windows only, run vpnup.bat before dialing to vpn, and run vpndown.bat after disconnected from the vpn.")
}

func generate_android(data []apnicData) {
	upfile := safeCreateFile("vpnup.sh")
	downfile := safeCreateFile("vpndown.sh")
	defer upfile.Close()
	defer downfile.Close()

	upfile.WriteString(android_upscript_header)
	downfile.WriteString(android_downscript_header)

	for _, v := range data {
		upstr := fmt.Sprintf("route add -net %s netmask %s gw $OLDGW\n", v.startIp, v.mask.String())
		upfile.WriteString(upstr)
		dnstr := fmt.Sprintf("route del -net %s netmask %s\n", v.startIp, v.mask.String())
		downfile.WriteString(dnstr)
	}

	fmt.Println("Old school way to call up/down script from openvpn client. use the regular openvpn 2.1 method to add routes if it's possible")
}

func fetch_ip_data() []apnicData {
	// fetch data from apnic
	fmt.Println("Fetching data from apnic.net, it might take a few minutes, please wait...")

	url := "http://ftp.apnic.net/apnic/stats/apnic/delegated-apnic-latest"
	resp, err := http.Get(url)
	if err != nil {
		fmt.Println(err.Error())
		os.Exit(-1)
	}
	defer resp.Body.Close()

	br := bufio.NewReader(resp.Body)
	reg, _ := regexp.Compile(`(apnic\|CN\|ipv4\|)([0-9.]*)\|([0-9]*)\|([0-9]*)\|(a.*)`)
	results := make([]apnicData, 0)

	for {
		line, isPrefix, err := br.ReadLine()
		if err != nil {
			if err != io.EOF {
				fmt.Println(err.Error())
				os.Exit(-1)
			}
			break
		}

		if isPrefix {
			fmt.Println("You should not see this!")
			return results
		}

		matches := reg.FindStringSubmatch(string(line))
		if len(matches) != 6 {
			continue
		}

		starting_ip := matches[2]
		num_ip, _ := strconv.Atoi(matches[3])

		imask := UintToIP(0xffffffff ^ uint32(num_ip-1))
		imaskNum := 32 - int(math.Log2(float64(num_ip)))
		// fmt.Printf("%s %v %d\n", starting_ip, imask, imaskNum)
		results = append(results, apnicData{starting_ip, imask, imaskNum})
	}
	return results
}

func UintToIP(ip uint32) net.IP {
	result := make(net.IP, 4)
	binary.BigEndian.PutUint32([]byte(result), ip)
	return result
}

func safeCreateFile(name string) *os.File {
	fp, err := os.Create(name)
	if err != nil {
		fmt.Println(err.Error())
		os.Exit(-1)
	}
	return fp
}
