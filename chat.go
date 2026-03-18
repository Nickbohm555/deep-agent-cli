package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/joho/godotenv"
	"github.com/openai/openai-go/v3"
)

func main() {
	verbose := flag.Bool("verbose", false, "enable verbose logging")
	modelFlag := flag.String("model", "", "override model (e.g. gpt-4o-mini)")
	flag.Parse()

	if *verbose {
		log.SetOutput(os.Stderr)
		log.SetFlags(log.LstdFlags | log.Lshortfile)
		log.Println("Verbose logging enabled")
	} else {
		log.SetOutput(os.Stdout)
		log.SetFlags(0)
		log.SetPrefix("")
	}

	_ = godotenv.Load()

	client := openai.NewClient()
	if *verbose {
		log.Println("OpenAI client initialized")
	}

	scanner := bufio.NewScanner(os.Stdin)
	getUserMessage := func() (string, bool) {
		if !scanner.Scan() {
			return "", false
		}
		return scanner.Text(), true
	}

	model := openai.ChatModelGPT5_2
	if *modelFlag != "" {
		model = openai.ChatModel(*modelFlag)
	}

	agent := NewAgent(&client, getUserMessage, *verbose, model)
	err := agent.Run(context.TODO())
	if err != nil {
		fmt.Printf("Error: %s\n", err.Error())
	}
}

func NewAgent(client *openai.Client, getUserMessage func() (string, bool), verbose bool, model openai.ChatModel) *Agent {
	return &Agent{
		client:         client,
		getUserMessage: getUserMessage,
		verbose:        verbose,
		model:          model,
	}
}

type Agent struct {
	client         *openai.Client
	getUserMessage func() (string, bool)
	verbose        bool
	model          openai.ChatModel
}

func (a *Agent) Run(ctx context.Context) error {
	conversation := []openai.ChatCompletionMessageParamUnion{}

	if a.verbose {
		log.Println("Starting chat session")
	}
	fmt.Println("Chat with OpenAI (use 'ctrl-c' to quit)")

	for {
		fmt.Print("\u001b[94mYou\u001b[0m: ")
		userInput, ok := a.getUserMessage()
		if !ok {
			if a.verbose {
				log.Println("User input ended, breaking from chat loop")
			}
			break
		}

		// Skip empty messages
		if userInput == "" {
			if a.verbose {
				log.Println("Skipping empty message")
			}
			continue
		}

		if a.verbose {
			log.Printf("User input received: %q", userInput)
		}

		userMessage := openai.UserMessage(userInput)
		conversation = append(conversation, userMessage)

		if a.verbose {
			log.Printf("Sending message to OpenAI, conversation length: %d", len(conversation))
		}

		message, err := a.runInference(ctx, conversation)
		if err != nil {
			if a.verbose {
				log.Printf("Error during inference: %v", err)
			}
			return err
		}
		if len(message.Choices) == 0 {
			return fmt.Errorf("no choices in response")
		}
		conversation = append(conversation, openai.AssistantMessage(message.Choices[0].Message.Content))

		if a.verbose {
			log.Printf("Received response from OpenAI")
		}

		fmt.Printf("\u001b[93mAssistant\u001b[0m: %s\n", message.Choices[0].Message.Content)
	}

	if a.verbose {
		log.Println("Chat session ended")
	}
	return nil
}

func (a *Agent) runInference(ctx context.Context, conversation []openai.ChatCompletionMessageParamUnion) (*openai.ChatCompletion, error) {
	if a.verbose {
		log.Printf("Making API call to OpenAI with model: %s", a.model)
	}

	message, err := a.client.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
		Model:    a.model,
		Messages: conversation,
	})

	if a.verbose {
		if err != nil {
			log.Printf("API call failed: %v", err)
		} else {
			log.Printf("API call successful, response received")
		}
	}

	return message, err
}
