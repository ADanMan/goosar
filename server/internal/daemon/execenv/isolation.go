package execenv

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os/exec"
	"strings"
	"time"
)

const PreparationHelperArg = "__goosar_execenv_prepare"

const (
	preparationActionPrepare = "prepare"
	preparationActionReuse   = "reuse"
	preparationWaitDelay     = 2 * time.Second
)

type preparationRequest struct {
	Action  string         `json:"action"`
	Prepare *PrepareParams `json:"prepare,omitempty"`
	Reuse   *ReuseParams   `json:"reuse,omitempty"`
}

type preparationOpenclawGatewayPin OpenclawGatewayPin

type preparationPrepareParams struct {
	*PrepareParams
	OpenclawGateway preparationOpenclawGatewayPin `json:"OpenclawGateway"`
}

type preparationReuseParams struct {
	*ReuseParams
	OpenclawGateway preparationOpenclawGatewayPin `json:"OpenclawGateway"`
}

type preparationRequestPayload struct {
	Action  string                    `json:"action"`
	Prepare *preparationPrepareParams `json:"prepare,omitempty"`
	Reuse   *preparationReuseParams   `json:"reuse,omitempty"`
}

type preparationResponse struct {
	Environment *Environment `json:"environment,omitempty"`
	Error       string       `json:"error,omitempty"`
}

func PrepareIsolated(ctx context.Context, command []string, params PrepareParams, logger *slog.Logger) (*Environment, error) {
	return runPreparationProcess(ctx, command, preparationRequest{
		Action:  preparationActionPrepare,
		Prepare: &params,
	}, logger)
}

func ReuseIsolated(ctx context.Context, command []string, params ReuseParams, logger *slog.Logger) (*Environment, error) {
	return runPreparationProcess(ctx, command, preparationRequest{
		Action: preparationActionReuse,
		Reuse:  &params,
	}, logger)
}

func runPreparationProcess(ctx context.Context, command []string, request preparationRequest, logger *slog.Logger) (*Environment, error) {
	if len(command) == 0 || strings.TrimSpace(command[0]) == "" {
		return nil, errors.New("execenv: preparation helper command is empty")
	}
	if ctx.Err() != nil {
		return nil, context.Cause(ctx)
	}
	payload, err := marshalPreparationRequest(request)
	if err != nil {
		return nil, fmt.Errorf("execenv: encode preparation request: %w", err)
	}

	cmd := exec.Command(command[0], command[1:]...)
	controller, err := newPreparationProcessController(cmd)
	if err != nil {
		return nil, fmt.Errorf("execenv: create preparation process controller: %w", err)
	}
	defer controller.close()
	cmd.WaitDelay = preparationWaitDelay
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("execenv: create preparation stdin: %w", err)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		stdin.Close()
		return nil, fmt.Errorf("execenv: start preparation helper: %w", err)
	}

	if err := controller.attach(cmd); err != nil {
		stdin.Close()
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		_ = controller.finish()
		return nil, fmt.Errorf("execenv: attach preparation helper process: %w", err)
	}

	writeDone := make(chan error, 1)
	go func() {
		_, writeErr := stdin.Write(payload)
		closeErr := stdin.Close()
		writeDone <- errors.Join(writeErr, closeErr)
	}()
	waitDone := make(chan error, 1)
	go func() { waitDone <- cmd.Wait() }()

	var stopErr error
	select {
	case err = <-waitDone:
	case <-ctx.Done():
		stopErr = controller.stop(cmd)
		err = <-waitDone
	}
	writeErr := <-writeDone
	finishErr := controller.finish()
	if lifecycleErr := errors.Join(stopErr, finishErr); lifecycleErr != nil {
		return nil, fmt.Errorf("execenv: stop preparation process tree: %w", lifecycleErr)
	}

	if ctx.Err() != nil {
		return nil, context.Cause(ctx)
	}
	if err != nil {
		if detail := strings.TrimSpace(stderr.String()); detail != "" {
			return nil, fmt.Errorf("execenv: preparation helper failed: %w: %s", err, detail)
		}
		return nil, fmt.Errorf("execenv: preparation helper failed: %w", err)
	}
	if writeErr != nil {
		return nil, fmt.Errorf("execenv: write preparation request: %w", writeErr)
	}

	var response preparationResponse
	if err := json.Unmarshal(stdout.Bytes(), &response); err != nil {
		return nil, fmt.Errorf("execenv: decode preparation response: %w", err)
	}
	if response.Error != "" {
		return nil, errors.New(response.Error)
	}
	if response.Environment != nil {

		response.Environment.logger = logger
	}
	return response.Environment, nil
}

func marshalPreparationRequest(request preparationRequest) ([]byte, error) {
	payload := preparationRequestPayload{Action: request.Action}
	if request.Prepare != nil {
		payload.Prepare = &preparationPrepareParams{
			PrepareParams:   request.Prepare,
			OpenclawGateway: preparationOpenclawGatewayPin(request.Prepare.OpenclawGateway),
		}
	}
	if request.Reuse != nil {
		payload.Reuse = &preparationReuseParams{
			ReuseParams:     request.Reuse,
			OpenclawGateway: preparationOpenclawGatewayPin(request.Reuse.OpenclawGateway),
		}
	}
	return json.Marshal(payload)
}

func decodePreparationRequest(in io.Reader) (preparationRequest, error) {
	var request preparationRequest
	decoder := json.NewDecoder(in)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		return preparationRequest{}, err
	}
	return request, nil
}

func RunPreparationHelper(in io.Reader, out io.Writer, logger *slog.Logger) error {
	request, err := decodePreparationRequest(in)
	if err != nil {
		return fmt.Errorf("decode preparation request: %w", err)
	}

	var response preparationResponse
	switch request.Action {
	case preparationActionPrepare:
		if request.Prepare == nil || request.Reuse != nil {
			return errors.New("invalid prepare request")
		}
		env, err := Prepare(*request.Prepare, logger)
		response.Environment = env
		if err != nil {
			response.Error = err.Error()
		}
	case preparationActionReuse:
		if request.Reuse == nil || request.Prepare != nil {
			return errors.New("invalid reuse request")
		}
		response.Environment = Reuse(*request.Reuse, logger)
	default:
		return fmt.Errorf("unknown preparation action %q", request.Action)
	}

	if err := json.NewEncoder(out).Encode(response); err != nil {
		return fmt.Errorf("encode preparation response: %w", err)
	}
	return nil
}
