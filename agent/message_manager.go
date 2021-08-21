// Contains message queue manager to flush / sends messages in the local database
package main

import (
	"encoding/json"
	"ghost/agent/client"
	"strings"
	"time"
)

// MessageQueueManager processes messages in the message queue - should run in its own go routine
func MessageQueueManager(client *client.Client) {
	// run forever
	for {

		// get a message from queue
		messages, rowIds, err := client.LocalDb.MessageQueueSelectURI("/core/pluginlog/")
		if err != nil {
			client.Log.Error("Error reading message queue: %v", err)
		}

		// sleep if we have no messages
		if len(messages) == 0 {
			client.LocalDb.Vacuum() // clean up db
			time.Sleep(client.PollTime)
			continue
		}

		// create marshal message
		msgBytes, err := json.Marshal(messages)
		if err != nil {
			client.Log.Error("Unable to marshal message: %v", err)
			// remove messages
			if n, err := client.LocalDb.MessageQueueDelete(rowIds); err != nil {
				client.Log.Error("Unable to remove messages: %v", err)
			} else {
				client.Log.Debug("Removed %v messages from message_queue", n)
			}
			time.Sleep(client.PollTime)
			continue
		}

		// send messages
		_, err = client.Sender.Send(msgBytes, "/core/pluginlog/")

		//handle possible errors
		if err != nil {

			// check for a bad status code
			if strings.Contains(err.Error(), "500 Internal Server Error") || strings.Contains(err.Error(), "400 Bad Request") {
				// remove the message if we get a bad status code
				client.Log.Error("Received bad status code from server, %v. Removing message from queue", err.Error())
				if n, err := client.LocalDb.MessageQueueDelete(rowIds); err != nil {
					client.Log.Error("Unable to remove messages: %v", err)
				} else {
					client.Log.Debug("Removed %v messages from message_queue", n)
				}

			} else {
				// some other error occured (network related), let's just wait and try again
				client.Log.Debug("Controller unreachable: %v", err)
			}

		} else {
			// everything is good. Let's remove the messages from the local database
			client.Log.Debug("Successfully sent %v messages to controller", len(messages))
			if n, err := client.LocalDb.MessageQueueDelete(rowIds); err != nil {
				client.Log.Error("Unable to remove messages: %v", err)
			} else {
				client.Log.Debug("Removed %v messages from message_queue", n)
			}
		}

		// sleep longer messages are less than 100
		if len(messages) >= 100 {
			time.Sleep(time.Second * 1)
			continue
		} else {
			client.LocalDb.Vacuum() // clean up db
			time.Sleep(client.PollTime)
		}

	}
}
