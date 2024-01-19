package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/olaola-chat/ra-comfy2go/client"
	"github.com/schollz/progressbar/v3"
)

// process CLI arguments
func procCLI() (string, int, string) {
	serverAddress := flag.String("address", "localhost", "Server address")
	serverPort := flag.Int("port", 8188, "Server port")
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), "Usage of %s:\n", os.Args[0])
		fmt.Printf("  %s [OPTIONS] filename", os.Args[0])
		fmt.Println("\nOptions:")
		flag.PrintDefaults()
		fmt.Println("\nfilename: Path to workflow json file")
	}
	flag.Parse()

	// Check for required filename argument
	if len(flag.Args()) != 1 {
		flag.Usage()
		os.Exit(1)
	}
	filename := flag.Arg(0)
	return *serverAddress, *serverPort, filename
}

func main() {
	clientaddr, clientport, workflow := procCLI()

	// callbacks can be used respond to QueuedItem updates, or client status changes
	callbacks := &client.ComfyClientCallbacks{
		ClientQueueCountChanged: func(c *client.ComfyClient, queuecount int) {
			log.Printf("Client %s at %s Queue size: %d", c.ClientID(), clientaddr, queuecount)
		},
		QueuedItemStarted: func(c *client.ComfyClient, qi *client.QueueItem) {
			log.Printf("Queued item %s started\n", qi.PromptID)
		},
		QueuedItemStopped: func(cc *client.ComfyClient, qi *client.QueueItem, reason client.QueuedItemStoppedReason) {
			log.Printf("Queued item %s stopped\n", qi.PromptID)
		},
		QueuedItemDataAvailable: func(cc *client.ComfyClient, qi *client.QueueItem, pmd *client.PromptMessageData) {
			log.Printf("image data available:\n")
			for _, v := range pmd.Images {
				log.Printf("\tFilename: %s Subfolder: %s Type: %s\n", v.Filename, v.Subfolder, v.Type)
			}
		},
	}

	// create a client
	c := client.NewComfyClient(clientaddr, clientport, callbacks)

	// the client needs to be in an initialized state before usage
	if !c.IsInitialized() {
		log.Printf("Initialize Client with ID: %s\n", c.ClientID())
		err := c.Init()
		if err != nil {
			log.Println("Error initializing client:", err)
			os.Exit(1)
		}
	}

	// load the workflow
	graph, _, err := c.NewGraphFromJsonFile(workflow)
	if err != nil {
		log.Println("Error loading graph JSON:", err)
		os.Exit(1)
	}

	// Get the nodes that are within the "API" Group.  GetSimpleAPI takes each
	// node and exposes it's first (and only it's first) property, with the title of the node as the key
	// in the Properties field.
	simple_api := graph.GetSimpleAPI()
	width := simple_api.Properties["Width"]
	height := simple_api.Properties["Height"]
	positive := simple_api.Properties["Positive"]
	negative := simple_api.Properties["Negative"]
	width.SetValue(1024)
	height.SetValue(1024)
	positive.SetValue("a dive bar, dimly lit, zombies, dancing, mosh pit, (kittens:1.5)")
	negative.SetValue("text, watermark")

	// or we can set it directly
	simple_api.Properties["Seed"].SetValue(2290222)

	// queue the prompt and get the resulting image
	item, err := c.QueuePrompt(graph)
	if err != nil {
		log.Println("Failed to queue prompt:", err)
		os.Exit(1)
	}

	// we'll provide a progress bar
	var bar *progressbar.ProgressBar = nil

	// continuously read messages from the QueuedItem until we get the "stopped" message type
	var currentNodeTitle string
	for continueLoop := true; continueLoop; {
		msg := <-item.Messages
		switch msg.Type {
		case "started":
			qm := msg.ToPromptMessageStarted()
			log.Printf("Start executing prompt ID %s\n", qm.PromptID)
		case "executing":
			bar = nil
			qm := msg.ToPromptMessageExecuting()
			// store the node's title so we can use it in the progress bar
			currentNodeTitle = qm.Title
			log.Printf("Executing Node: %d\n", qm.NodeID)
		case "progress":
			// update our progress bar
			qm := msg.ToPromptMessageProgress()
			if bar == nil {
				bar = progressbar.Default(int64(qm.Max), currentNodeTitle)
			}
			bar.Set(qm.Value)
		case "stopped":
			// if we were stopped for an exception, display the exception message
			qm := msg.ToPromptMessageStopped()
			if qm.Exception != nil {
				log.Println(qm.Exception)
				os.Exit(1)
			}
			continueLoop = false
		case "data":
			qm := msg.ToPromptMessageData()
			for _, v := range qm.Images {
				img_data, err := c.GetImage(v)
				if err != nil {
					log.Println("Failed to get image:", err)
					os.Exit(1)
				}
				f, err := os.Create(v.Filename)
				if err != nil {
					log.Println("Failed to write image:", err)
					os.Exit(1)
				}
				f.Write(*img_data)
				f.Close()
				log.Println("Got image: ", v.Filename)
			}
		}
	}
}
