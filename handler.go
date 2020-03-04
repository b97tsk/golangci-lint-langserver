package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"

	"github.com/sourcegraph/jsonrpc2"
)

func NewHandler(logger logger) jsonrpc2.Handler {
	handler := &langHandler{
		logger:  logger,
		request: make(chan DocumentURI),
	}
	go handler.linter()

	return jsonrpc2.HandlerWithError(handler.handle)
}

type langHandler struct {
	logger  logger
	conn    *jsonrpc2.Conn
	request chan DocumentURI

	rootURI string
}

//nolint:unparam
func (h *langHandler) lint(uri DocumentURI) ([]Diagnostic, error) {
	h.logger.Printf("golangci-lint-langserver: uri: %s", uri)

	cmd := exec.Command("golangci-lint", "run", "--enable-all", "--out-format", "json")
	b, err := cmd.CombinedOutput()
	if err == nil {
		return nil, nil
	}

	h.logger.Printf("%v", b)

	var result GolangCILintResult
	if err := json.Unmarshal(b, &result); err != nil {
		return nil, err
	}

	h.logger.DebugJSON("golangci-lint-langserver: result:", result)

	diagnostics := make([]Diagnostic, 0)
	for _, issue := range result.Issues {
		issue := issue

		if !strings.HasSuffix(string(uri), issue.Pos.Filename) {
			continue
		}

		//nolint:gomnd
		d := Diagnostic{
			Range: Range{
				Start: Position{Line: issue.Pos.Line - 1, Character: issue.Pos.Column - 1},
				End:   Position{Line: issue.Pos.Line - 1, Character: issue.Pos.Column - 1},
			},
			Severity: 1,
			Source:   &issue.FromLinter,
			Message:  issue.Text,
		}
		diagnostics = append(diagnostics, d)
	}

	return diagnostics, nil
}

func (h *langHandler) linter() {
	for {
		uri, ok := <-h.request
		if !ok {
			break
		}

		diagnostics, err := h.lint(uri)
		if err != nil {
			h.logger.Printf("%s", err)
			continue
		}

		h.logger.DebugJSON("hoge:", diagnostics)

		if err := h.conn.Notify(
			context.Background(),
			"textDocument/publishDiagnostics",
			&PublishDiagnosticsParams{
				URI:         uri,
				Diagnostics: diagnostics,
			}); err != nil {
			h.logger.Printf("%s", err)
		}
	}
}

func (h *langHandler) handle(ctx context.Context, conn *jsonrpc2.Conn, req *jsonrpc2.Request) (result interface{}, err error) {
	h.logger.DebugJSON("golangci-lint-langserver: request:", req)

	switch req.Method {
	case "initialize":
		return h.handleInitialize(ctx, conn, req)
	case "initialized":
		return
	case "shutdown":
		return h.handleShutdown(ctx, conn, req)
	case "textDocument/didOpen":
		return h.handleTextDocumentDidOpen(ctx, conn, req)
	case "textDocument/didClose":
		return h.handleTextDocumentDidClose(ctx, conn, req)
	case "textDocument/didChange":
		return h.handleTextDocumentDidChange(ctx, conn, req)
	case "textDocument/didSave":
		return h.handleTextDocumentDidSave(ctx, conn, req)
	}

	return nil, &jsonrpc2.Error{Code: jsonrpc2.CodeMethodNotFound, Message: fmt.Sprintf("method not supported: %s", req.Method)}
}

func (h *langHandler) handleInitialize(_ context.Context, conn *jsonrpc2.Conn, req *jsonrpc2.Request) (result interface{}, err error) {
	var params InitializeParams
	if err := json.Unmarshal(*req.Params, &params); err != nil {
		return nil, err
	}

	h.rootURI = params.RootURI
	h.conn = conn

	return InitializeResult{
		Capabilities: ServerCapabilities{
			TextDocumentSync: TDSKFull,
		},
	}, nil
}

func (h *langHandler) handleShutdown(_ context.Context, _ *jsonrpc2.Conn, _ *jsonrpc2.Request) (result interface{}, err error) {
	close(h.request)
	return nil, nil
}

func (h *langHandler) handleTextDocumentDidOpen(_ context.Context, _ *jsonrpc2.Conn, req *jsonrpc2.Request) (result interface{}, err error) {
	var params DidOpenTextDocumentParams
	if err := json.Unmarshal(*req.Params, &params); err != nil {
		return nil, err
	}

	h.request <- params.TextDocument.URI

	return nil, nil
}

func (h *langHandler) handleTextDocumentDidClose(_ context.Context, _ *jsonrpc2.Conn, _ *jsonrpc2.Request) (result interface{}, err error) {
	return nil, nil
}

func (h *langHandler) handleTextDocumentDidChange(_ context.Context, _ *jsonrpc2.Conn, _ *jsonrpc2.Request) (result interface{}, err error) {
	return nil, nil
}

func (h *langHandler) handleTextDocumentDidSave(_ context.Context, _ *jsonrpc2.Conn, req *jsonrpc2.Request) (result interface{}, err error) {
	var params DidSaveTextDocumentParams
	if err := json.Unmarshal(*req.Params, &params); err != nil {
		return nil, err
	}

	h.request <- params.TextDocument.URI

	return nil, nil
}
