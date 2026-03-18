## App Build + Debug Instructions (Operational)

If you need to test something requiring an LLM key, use `OPENAI_API_KEY` from `keys.txt` only as a local reference, then export it into the environment before running tests.

## Runtime Stack (Current Plan)

## Go Run + Terminal Interaction

Use a generic run command for any CLI entrypoint in this repo:

```bash
go run <tool>.go [flags]
```

To test end-to-end behavior, simulate the user by typing directly in the terminal after startup.
The app should print a `You:` prompt, accept your message, and return an assistant response.

Example:

```bash
go run read.go -verbose
# wait for: Chat with OpenAI + tools ...
You: what is in /path/to/file.txt
# confirm tool logs/response appear after your message
```
