package executor

// DefaultStreamBufferSize is 20MB - maximum buffer for SSE stream scanning.
// This size accommodates large tool call responses from LLM providers.
const DefaultStreamBufferSize = 20 * 1024 * 1024
