package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/invopop/jsonschema"
	"github.com/joho/godotenv"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/shared"
)

func main() {
	verbose := flag.Bool("verbose", false, "enable verbose logging")
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
	if os.Getenv("OPENAI_API_KEY") == "" {
		log.Fatal("OPENAI_API_KEY is not set")
	}

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

	tools := []ToolDefinition{ReadFileDefinition, ListFilesDefinition}
	if *verbose {
		log.Printf("Initialized %d tools", len(tools))
	}
	agent := NewAgent(&client, getUserMessage, tools, *verbose, openai.ChatModelGPT5_2)
	err := agent.Run(context.TODO())
	if err != nil {
		fmt.Printf("Error: %s\n", err.Error())
	}
}

func NewAgent(
	client *openai.Client,
	getUserMessage func() (string, bool),
	tools []ToolDefinition,
	verbose bool,
	model openai.ChatModel,
) *Agent {
	return &Agent{
		client:         client,
		getUserMessage: getUserMessage,
		tools:          tools,
		verbose:        verbose,
		model:          model,
	}
}

type Agent struct {
	client         *openai.Client
	getUserMessage func() (string, bool)
	tools          []ToolDefinition
	verbose        bool
	model          openai.ChatModel
}

func (a *Agent) Run(ctx context.Context) error {
	conversation := []openai.ChatCompletionMessageParamUnion{}

	if a.verbose {
		log.Println("Starting chat session with tools enabled")
	}
	fmt.Println("Chat with OpenAI + tools (use 'ctrl-c' to quit)")

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

		completion, err := a.runInference(ctx, conversation)
		if err != nil {
			if a.verbose {
				log.Printf("Error during inference: %v", err)
			}
			return err
		}
		if len(completion.Choices) == 0 {
			return fmt.Errorf("no choices in response")
		}
		message := completion.Choices[0].Message
		conversation = append(conversation, message.ToParam())

		// Keep processing until the model stops using tools.
		for {
			// Collect all tool uses and their results.
			var toolResults []openai.ChatCompletionMessageParamUnion
			var hasToolUse bool

			if a.verbose {
				log.Printf("Processing response content and %d tool calls", len(message.ToolCalls))
			}

			if message.Content != "" {
				fmt.Printf("\u001b[93mAssistant\u001b[0m: %s\n", message.Content)
			}

			for _, toolCall := range message.ToolCalls {
				if toolCall.Type != "function" {
					continue
				}

				hasToolUse = true
				functionCall := toolCall.AsFunction()
				if a.verbose {
					log.Printf("Tool use detected: %s with input: %s", functionCall.Function.Name, functionCall.Function.Arguments)
				}
				fmt.Printf("\u001b[96mtool\u001b[0m: %s(%s)\n", functionCall.Function.Name, functionCall.Function.Arguments)

				// Find and execute the tool.
				var toolResult string
				var toolError error
				var toolFound bool
				for _, tool := range a.tools {
					if tool.Name == functionCall.Function.Name {
						if a.verbose {
							log.Printf("Executing tool: %s", tool.Name)
						}
						toolResult, toolError = tool.Function(json.RawMessage(functionCall.Function.Arguments))
						fmt.Printf("\u001b[92mresult\u001b[0m: %s\n", toolResult)
						if toolError != nil {
							fmt.Printf("\u001b[91merror\u001b[0m: %s\n", toolError.Error())
						}
						if a.verbose {
							if toolError != nil {
								log.Printf("Tool execution failed: %v", toolError)
							} else {
								log.Printf("Tool execution successful, result length: %d chars", len(toolResult))
							}
						}
						toolFound = true
						break
					}
				}

				if !toolFound {
					toolError = fmt.Errorf("tool '%s' not found", functionCall.Function.Name)
					fmt.Printf("\u001b[91merror\u001b[0m: %s\n", toolError.Error())
				}

				// Add tool result to collection.
				if toolError != nil {
					toolResults = append(toolResults, openai.ToolMessage(toolError.Error(), functionCall.ID))
				} else {
					toolResults = append(toolResults, openai.ToolMessage(toolResult, functionCall.ID))
				}
			}

			// If there were no tool uses, we're done.
			if !hasToolUse {
				break
			}

			// Send all tool results back and get the model's response.
			if a.verbose {
				log.Printf("Sending %d tool results back to OpenAI", len(toolResults))
			}
			conversation = append(conversation, toolResults...)

			// Get response after tool execution.
			completion, err = a.runInference(ctx, conversation)
			if err != nil {
				if a.verbose {
					log.Printf("Error during followup inference: %v", err)
				}
				return err
			}
			if len(completion.Choices) == 0 {
				return fmt.Errorf("no choices in response")
			}
			message = completion.Choices[0].Message
			conversation = append(conversation, message.ToParam())

			if a.verbose {
				log.Printf("Received followup response")
			}

			// Continue loop to process the new message.
		}
	}

	if a.verbose {
		log.Println("Chat session ended")
	}
	return nil
}

func (a *Agent) runInference(ctx context.Context, conversation []openai.ChatCompletionMessageParamUnion) (*openai.ChatCompletion, error) {
	openAITools := []openai.ChatCompletionToolUnionParam{}
	for _, tool := range a.tools {
		openAITools = append(openAITools, openai.ChatCompletionFunctionTool(openai.FunctionDefinitionParam{
			Name:        tool.Name,
			Description: openai.Opt(tool.Description),
			Parameters:  tool.InputSchema,
		}))
	}

	if a.verbose {
		log.Printf("Making API call to OpenAI with model: %s and %d tools", a.model, len(openAITools))
	}

	message, err := a.client.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
		Model:    a.model,
		Messages: conversation,
		Tools:    openAITools,
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

type ToolDefinition struct {
	Name        string                    `json:"name"`
	Description string                    `json:"description"`
	InputSchema shared.FunctionParameters `json:"input_schema"`
	Function    func(input json.RawMessage) (string, error)
}

var ReadFileDefinition = ToolDefinition{
	Name:        "read_file",
	Description: "Read the contents of a given relative file path. Use this when you want to see what's inside a file. Do not use this with directory names.",
	InputSchema: ReadFileInputSchema,
	Function:    ReadFile,
}

var ListFilesDefinition = ToolDefinition{
	Name:        "list_files",
	Description: "List files and directories at a given path. If no path is provided, lists files in the current directory.",
	InputSchema: ListFilesInputSchema,
	Function:    ListFiles,
}

type ReadFileInput struct {
	Path string `json:"path" jsonschema:"required" jsonschema_description:"The relative path of a file in the working directory."`
}

var ReadFileInputSchema = GenerateSchema[ReadFileInput]()

type ListFilesInput struct {
	Path string `json:"path,omitempty" jsonschema_description:"Optional relative path to list files from. Defaults to current directory if not provided."`
}

var ListFilesInputSchema = GenerateSchema[ListFilesInput]()

func ReadFile(input json.RawMessage) (string, error) {
	readFileInput := ReadFileInput{}
	err := json.Unmarshal(input, &readFileInput)
	if err != nil {
		return "", err
	}

	if readFileInput.Path == "" {
		return "", fmt.Errorf("path is required")
	}

	fileInfo, err := os.Stat(readFileInput.Path)
	if err != nil {
		return "", err
	}
	if fileInfo.IsDir() {
		return "", fmt.Errorf("path points to a directory, not a file")
	}

	log.Printf("Reading file: %s", readFileInput.Path)
	content, err := os.ReadFile(readFileInput.Path)
	if err != nil {
		log.Printf("Failed to read file %s: %v", readFileInput.Path, err)
		return "", err
	}
	log.Printf("Successfully read file %s (%d bytes)", readFileInput.Path, len(content))
	return string(content), nil
}

func ListFiles(input json.RawMessage) (string, error) {
	listFilesInput := ListFilesInput{}
	err := json.Unmarshal(input, &listFilesInput)
	if err != nil {
		return "", err
	}

	dir := "."
	if listFilesInput.Path != "" {
		dir = listFilesInput.Path
	}

	log.Printf("Listing files in directory: %s", dir)

	var files []string
	err = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		relPath, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}

		// Skip .devenv directory and its contents
		if info.IsDir() && (relPath == ".devenv" || strings.HasPrefix(relPath, ".devenv/")) {
			return filepath.SkipDir
		}

		if relPath != "." {
			if info.IsDir() {
				files = append(files, relPath+"/")
			} else {
				files = append(files, relPath)
			}
		}
		return nil
	})

	if err != nil {
		log.Printf("Failed to list files in %s: %v", dir, err)
		return "", err
	}

	log.Printf("Successfully listed %d items in %s", len(files), dir)

	result, err := json.Marshal(files)
	if err != nil {
		return "", err
	}

	return string(result), nil
}

func GenerateSchema[T any]() shared.FunctionParameters {
	reflector := jsonschema.Reflector{
		AllowAdditionalProperties: false,
		DoNotReference:            true,
	}
	var v T

	schema := reflector.Reflect(v)
	schemaJSON, err := json.Marshal(schema)
	if err != nil {
		panic(err)
	}
	out := map[string]any{}
	if err := json.Unmarshal(schemaJSON, &out); err != nil {
		panic(err)
	}
	return out
}
