package marketapp

import "errors"

var ErrExecutorUnavailable = errors.New("market action executor unavailable")

type ActionExecutionResult struct {
	ResultOK     *bool
	ResultReason *byte
	AuctionID    uint64
	Raw          interface{}
}

type ActionExecutor interface {
	Execute(action Action) (ActionExecutionResult, error)
	Close()
}

type ActionExecutorFactory interface {
	NewActionExecutor(cfg Config) ActionExecutor
}

type unsupportedActionExecutorFactory struct{}

func (unsupportedActionExecutorFactory) NewActionExecutor(cfg Config) ActionExecutor {
	return unsupportedActionExecutor{}
}

type unsupportedActionExecutor struct{}

func (unsupportedActionExecutor) Execute(action Action) (ActionExecutionResult, error) {
	return ActionExecutionResult{}, ErrExecutorUnavailable
}

func (unsupportedActionExecutor) Close() {}
