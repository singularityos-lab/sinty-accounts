package main

import (
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/godbus/dbus/v5"

	"github.com/singularityos-lab/sinty-accounts/accounts"
)

func connectBus(session bool) (*dbus.Conn, error) {
	if session {
		return dbus.ConnectSessionBus()
	}
	if os.Getenv("DBUS_SYSTEM_BUS_ADDRESS") == "" {
		os.Setenv("DBUS_SYSTEM_BUS_ADDRESS", "unix:path=/run/dbus/system_bus_socket")
	}
	deadline := time.Now().Add(60 * time.Second)
	for {
		conn, err := dbus.ConnectSystemBus()
		if err == nil {
			return conn, nil
		}
		if time.Now().After(deadline) {
			return nil, err
		}
		time.Sleep(200 * time.Millisecond)
	}
}

func main() {
	log.SetFlags(0)
	log.SetPrefix("sinty-accountsd: ")

	root := flag.String("root", "/", "root of the user database")
	interfaces := flag.String("interfaces", "/usr/share/accountsservice/interfaces", "directory of extension interface descriptions")
	session := flag.Bool("session-bus", false, "serve on the session bus, for testing")
	flag.Parse()

	conn, err := connectBus(*session)
	if err != nil {
		log.Fatalf("bus: %v", err)
	}
	defer conn.Close()

	svc := accounts.New(conn, accounts.Options{Root: *root, InterfacesDir: *interfaces})
	if err := svc.Export(); err != nil {
		log.Fatalf("export: %v", err)
	}

	reply, err := conn.RequestName(accounts.BusName, dbus.NameFlagDoNotQueue)
	if err != nil {
		log.Fatalf("request name %s: %v", accounts.BusName, err)
	}
	if reply != dbus.RequestNameReplyPrimaryOwner {
		log.Fatalf("%s already owned (accounts-daemon running?) - not starting", accounts.BusName)
	}

	stop := make(chan struct{})
	go svc.Watch(stop)

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
	close(stop)
}
