# llm-mux

**Free multi-format LLM gateway** - access any model through your preferred API format, without API keys.

Authenticate once with OAuth, then use OpenAI/Gemini/Claude/Ollama API formats to call any provider.

## Why llm-mux?

| Traditional API Access | llm-mux |
|------------------------|---------|
| Requires API keys | Uses OAuth from CLI tools |
| Pay per token | Free (within CLI quotas) |
| One provider per key | All providers, one gateway |
| Learn each provider's API | Use your preferred format |

## Quick Start

```bash
# Install
brew tap nghyane/tap && brew install llm-mux

# Authenticate with any provider
llm-mux --login              # Gemini CLI
llm-mux --antigravity-login  # Antigravity (Gemini + Claude + GPT-OSS)
llm-mux --copilot-login      # GitHub Copilot

# Start service
brew services start llm-mux

# Call Gemini using OpenAI format
curl http://localhost:8318/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{"model": "gemini-2.5-flash", "messages": [{"role": "user", "content": "Hello!"}]}'
```

## Supported Providers

### Google

| Provider | Login | Models |
|----------|-------|--------|
| **Gemini CLI** | `--login` | gemini-2.5-pro, gemini-2.5-flash, gemini-2.5-flash-lite, gemini-3-pro-preview |
| **Antigravity** | `--antigravity-login` | Gemini + Claude Sonnet/Opus 4.5 + GPT-OSS + Computer Use |
| **AI Studio** | `--login` | Gemini models + image generation |
| **Vertex AI** | API Key | Gemini models |

### Anthropic

| Provider | Login | Models |
|----------|-------|--------|
| **Claude** | `--claude-login` | claude-sonnet-4-5, claude-opus-4-5 |
| **Kiro** | `--kiro-login` | Claude models via Amazon Q |

### OpenAI

| Provider | Login | Models |
|----------|-------|--------|
| **Codex** | `--codex-login` | gpt-5.1, gpt-5.1-codex, gpt-5.1-codex-max |
| **GitHub Copilot** | `--copilot-login` | gpt-4.1, gpt-4o, gpt-5-mini, gpt-5.1-codex-max |

### Others

| Provider | Login | Models |
|----------|-------|--------|
| **iFlow** | `--iflow-login` | qwen3-coder-plus, deepseek-r1, kimi-k2, glm-4.6 |
| **Cline** | `--cline-login` | minimax-m2, grok-code-fast-1 |
| **Qwen** | `--qwen-login` | qwen3-coder-plus, qwen3-coder-flash |

## API Formats

Choose your preferred format - all formats can access all providers:

| Format | Endpoints |
|--------|-----------|
| **OpenAI** | `/v1/chat/completions`, `/v1/completions`, `/v1/models` |
| **Gemini** | `/v1beta/models/{model}:generateContent`, `/v1beta/models/{model}:streamGenerateContent` |
| **Claude** | `/v1/messages` |
| **Ollama** | `/api/chat`, `/api/generate`, `/api/tags` |

```bash
# Same model, different formats:

# OpenAI format
curl localhost:8318/v1/chat/completions -d '{"model":"gemini-2.5-flash",...}'

# Gemini format
curl localhost:8318/v1beta/models/gemini-2.5-flash:generateContent -d '{"contents":[...]}'

# Ollama format
curl localhost:8318/api/chat -d '{"model":"gemini-2.5-flash",...}'
```

## Architecture

```
  API Formats                      Providers
  ───────────                      ─────────
    OpenAI ───┐                 ┌─── Gemini CLI
    Claude ───┼─── Unified ─────┼─── Antigravity
    Gemini ───┤       IR        ├─── Claude/Kiro
    Ollama ───┘                 └─── Codex/Copilot/iFlow
```

- **IR Translation**: Each format converts to/from IR (2n translations instead of n²)
- **Tool Call Normalization**: Auto-fixes parameter mismatches (`filePath` ↔ `file_path`)
- **Dynamic Registry**: Tracks OAuth sessions, auto-hides models when quota exceeded

## Installation

### Homebrew
```bash
brew tap nghyane/tap
brew install llm-mux
brew services start llm-mux
```

### Docker
```bash
docker pull nghyane/llm-mux
docker run -p 8318:8318 -v ~/.config/llm-mux:/root/.config/llm-mux nghyane/llm-mux
```

### From Source
```bash
go build -o llm-mux ./cmd/server/
./llm-mux -config config.yaml
```

## Configuration

```yaml
port: 8318
auth-dir: "~/.config/llm-mux/auth"
use-canonical-translator: true
```

Tokens stored in `~/.config/llm-mux/auth/` with auto-refresh.

## How It Works

1. **OAuth Capture**: Performs same OAuth flow as official CLI tools
2. **Token Management**: Stores and auto-refreshes tokens
3. **Format Translation**: Parses request (any format) → IR → provider-native format
4. **Response Translation**: Provider response → IR → original request format
5. **Load Balancing**: Routes to available OAuth sessions, handles quota limits

## License

MIT License - see [LICENSE](LICENSE)
