package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"

	"tsunami/client"
)

func run(args []string, parameter *client.Parameter, session **client.Session) error {
	cmd := args[0]
	switch cmd {
	case "close":
		return client.CommandClose(*session)
	case "connect":
		s, err := client.CommandConnect(parameter, args[1:])
		if err != nil {
			return err
		}
		*session = s
	case "get":
		if len(args) != 2 {
			return errors.New("need get args")
		}
		return client.CommandGet(args[1], *session)
	case "dir":
		return client.CommandDir(*session)
	case "help":
		client.CommandHelp(args[1:])
	case "quit":
		fallthrough
	case "exit":
		fallthrough
	case "bye":
		client.CommandQuit(*session)
	case "set":
		setArgs := args[1:]
		if len(setArgs) == 1 && strings.Contains(setArgs[0], " ") {
			setArgs = strings.Split(setArgs[0], " ")
		}
		return client.CommandSet(parameter, setArgs)

	default:
		fmt.Fprintf(os.Stderr, "Unrecognized command: '%v'.  Use 'HELP' for help.\n\n", cmd)
	}
	return nil
}

func main() {
	fmt.Fprintf(os.Stderr, "Tsunami Go Client for protocol rev %X\nRevision: %v\n", client.PROTOCOL_REVISION, 0.1)
	parameter := client.NewParameter()
	var session *client.Session
	flag.Parse()
	args := flag.Args()
	if len(args) == 0 {
		for {
			fmt.Print("tsunami> ")
			reader := bufio.NewReader(os.Stdin)
			line, _, err := reader.ReadLine()
			if err != nil {
				os.Exit(1)
			}
			if len(line) == 0 {
				continue
			}
			args := strings.SplitN(string(line), " ", 2)
			err = run(args, parameter, &session)
			if err != nil {
				fmt.Println(err)
			}
		}
	} else {
		run(args, parameter, &session)
	}
}
