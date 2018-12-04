package main

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/Damnever/goqueue"
)

// Wg is used as a WaitGroup
var Wg = &sync.WaitGroup{}
var maxConcurrency = runtime.NumCPU() * 2
var errored = goqueue.New(0)
var complete = goqueue.New(0)
var queue = goqueue.New(0)
var badDNS []Host
var timeoutHosts []Host
var authHosts []Host
var generalHosts []Host
var knifeHosts []Host

// Report is used for organizing Hosts
type Report struct {
	DNSHost      []Host `json:"dns_hosts"`
	AuthHosts    []Host `json:"auth_hosts"`
	TimeoutHosts []Host `json:"timeout_hosts"`
	GeneralHosts []Host `json:"general_hosts"`
	KnifeHosts   []Host `json:"knife_hosts"`
}

// Host is used for organizing Chef elements
type Host struct {
	Hostname string `json:"hostname"`
	Domain   string `json:"domain"`
	ChefEnv  string `json:"chefenv"`
	RunList  string `json:"runlist"`
}

// check is used to help catch all the craziness
func check(e error) {
	if e != nil {
		panic(e)
	}
}

func hostValidate(hosts []Host) {
	for _, element := range hosts {
		if element.Hostname == " " {
			log.Fatal("Please ensure the following entry contains a hostname:", element)
		}
		if element.Domain == " " {
			log.Fatal("Please ensure the following entry contains a domain:", element)
		}
		if element.ChefEnv == " " {
			log.Fatal("Please ensure the following entry contains a chef environment:", element)
		}
		if element.RunList == " " {
			log.Fatal("Please ensure the following entry contains a run list:", element)
		}
	}
}

func csvToHosts(csvFilename string) (hosts []Host) {
	file, err := os.Open(csvFilename)
	if err != nil {
		fmt.Println("Error:", err)
		return
	}
	defer func() {
		cerr := file.Close()
		check(cerr)
	}()
	reader := csv.NewReader(file)
	reader.Comma = '	'
	// read all records into memory
	for {
		line, error := reader.Read()
		if error == io.EOF {
			break
		} else if error != nil {
			log.Fatal(error)
		}
		hosts = append(hosts, Host{
			Hostname: line[0],
			Domain:   line[1],
			ChefEnv:  line[2],
			RunList:  line[3]},
		)
	}
	if err != nil {
		fmt.Println("Error:", err)
		return
	}
	hostValidate(hosts)
	return
}

func handleBootstrapError(out []byte, host Host, exitCode int) (bootstrapSuccess bool) {
	o := string(out)
	if strings.Contains(o, "Authentication failed") {
		authHosts = append(authHosts, host)
		return false
	}
	if strings.Contains(o, "ConnectionTimeout") {
		timeoutHosts = append(timeoutHosts, host)
		return false
	}
	if strings.Contains(o, "nodename nor servname provided") {
		badDNS = append(badDNS, host)
		return false
	}
	if exitCode == 1 {
		generalHosts = append(generalHosts, host)
		return false
	}
	if exitCode == 100 {
		knifeHosts = append(knifeHosts, host)
		return false
	}
	return true
}

func runCommand(cmd string) (out []byte, exitCode int) {
	c := exec.Command("sh", "-c", cmd)
	cmdOutput := &bytes.Buffer{}
	cmdErrorOutput := &bytes.Buffer{}
	c.Stdout = cmdOutput
	c.Stderr = cmdErrorOutput
	if err := c.Start(); err != nil {
		log.Fatalf("cmd.Start: %v", err)
	}
	if err := c.Wait(); err != nil {
		if exiterr, ok := err.(*exec.ExitError); ok {
			// The program has exited with an exit code != 0

			// This works on both Unix and Windows. Although package
			// syscall is generally platform dependent, WaitStatus is
			// defined for both Unix and Windows and in both cases has
			// an ExitStatus() method with the same signature.
			if status, ok := exiterr.Sys().(syscall.WaitStatus); ok {
				exitCode = status.ExitStatus()
			}
		} else {
			log.Fatalf("cmd.Wait: %v", err)
		}
	}
	var outputArray []byte
	outputArray = append(outputArray, cmdErrorOutput.Bytes()...)
	outputArray = append(outputArray, cmdOutput.Bytes()...)
	return outputArray, exitCode
}

func bootstrap(host Host) {
	cmd := generateCommand(host)
	cmdOut, exitCode := runCommand(cmd)
	filename := strings.Join([]string{"./logs/", host.Hostname, ".txt"}, "")
	err := ioutil.WriteFile(filename, cmdOut, 0644)
	check(err)
	handleBootstrapError(cmdOut, host, exitCode)
	return
}

func errorReport() (report Report) {

	for i := range badDNS {
		report.DNSHost = append(report.DNSHost, badDNS[i])
	}
	for i := range authHosts {
		report.AuthHosts = append(report.AuthHosts, authHosts[i])
	}
	for i := range timeoutHosts {
		report.TimeoutHosts = append(report.TimeoutHosts, timeoutHosts[i])
	}
	for i := range generalHosts {
		report.GeneralHosts = append(report.GeneralHosts, generalHosts[i])
	}
	for i := range knifeHosts {
		report.KnifeHosts = append(report.KnifeHosts, knifeHosts[i])
	}
	return report
}

func generateCommand(host Host) (cmd string) {
	fqdn := strings.Join([]string{host.Hostname, host.Domain}, ".")
	superuserName := os.Getenv("superuserName")
	superuserPw := os.Getenv("superuserPw")
	//sudo_value := os.Getenv("USE_SUDO")
	cmd = strings.Join([]string{"knife bootstrap ", fqdn, " -N ", host.Hostname, " -E ", host.ChefEnv, " --sudo", " --ssh-user ", superuserName, " --ssh-password ", superuserPw, " -r ", host.RunList}, "")
	return
}
func worker(queue *goqueue.Queue) {
	for !queue.IsEmpty() {
		//Get queue with 2 second timeout
		val, err := queue.Get(2)
		item := val.(Host)
		if err != nil {
			fmt.Println("Unexpect Error: \n", err)
		}
		bootstrap(item)
		if err != nil {
			errored.PutNoWait(val)
		} else {
			complete.PutNoWait(val)
		}
	}
	defer Wg.Done()
}

func main() {
	os.Mkdir("./logs", 0777)

	// Read in the csv and populate queue for workers
	var hosts []Host
	var csvFilename string

	flag.StringVar(&csvFilename, "file", "./sample.tsv", "file containing hosts to be bootstrapped")
	flag.Parse()
	hosts = csvToHosts(csvFilename)
	// Queue all records
	for i := range hosts {
		record := hosts[i]
		recordJSON, _ := json.Marshal(record)
		fmt.Println("Queueing:", string(recordJSON))
		queue.PutNoWait(record)
	}
	// Start worker pool
	for i := 0; i < maxConcurrency && !queue.IsEmpty(); i++ {
		Wg.Add(1)
		go worker(queue)
		// Sleep 50 Milliseconds to give worker time to start
		time.Sleep(50 * time.Millisecond)
	}
	Wg.Wait()
	if len(knifeHosts) > 0 || len(generalHosts) > 0 || len(badDNS) > 0 || len(timeoutHosts) > 0 || len(authHosts) > 0 {
		report := errorReport()
		r, _ := json.MarshalIndent(report, "", "  ")
		fmt.Println("Error Report:")
		fmt.Printf("%s/n", r)
	}
}
