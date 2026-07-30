package digest

import "time"

type TelegramOperation string

const (
	TelegramOperationPoll TelegramOperation = "getUpdates"
	TelegramOperationSend TelegramOperation = "sendMessage"
)

type TelegramPolicyAction string

const (
	TelegramPolicyBackoff            TelegramPolicyAction = "backoff"
	TelegramPolicyRetryDelivery      TelegramPolicyAction = "retry_delivery"
	TelegramPolicySuspendPoll        TelegramPolicyAction = "suspend_poll"
	TelegramPolicySuspendIntegration TelegramPolicyAction = "suspend_integration"
)

type TelegramPolicy struct {
	Action TelegramPolicyAction
	Delay  time.Duration
}

func TelegramPolicyFor(
	operation TelegramOperation,
	kind TelegramErrorKind,
	consecutiveFailures int,
	retryAfter time.Duration,
) (TelegramPolicy, bool) {
	if !knownTelegramErrorKind(kind) {
		return TelegramPolicy{}, false
	}
	switch operation {
	case TelegramOperationPoll:
		switch kind {
		case TelegramErrorNetwork, TelegramErrorTimeout, TelegramErrorServer:
			return TelegramPolicy{Action: TelegramPolicyBackoff, Delay: pollBackoff(consecutiveFailures)}, true
		case TelegramErrorRateLimited:
			delay := pollBackoff(consecutiveFailures)
			if retryAfter > delay {
				delay = retryAfter
			}
			return TelegramPolicy{Action: TelegramPolicyBackoff, Delay: delay}, true
		case TelegramErrorWebhookConflict, TelegramErrorClient, TelegramErrorProtocol:
			return TelegramPolicy{Action: TelegramPolicySuspendPoll, Delay: 5 * time.Minute}, true
		case TelegramErrorInvalidAuth:
			return TelegramPolicy{Action: TelegramPolicySuspendIntegration, Delay: 5 * time.Minute}, true
		}
	case TelegramOperationSend:
		switch kind {
		case TelegramErrorInvalidAuth:
			return TelegramPolicy{Action: TelegramPolicySuspendIntegration, Delay: 5 * time.Minute}, true
		case TelegramErrorWebhookConflict:
			return TelegramPolicy{Action: TelegramPolicyRetryDelivery}, true
		default:
			return TelegramPolicy{Action: TelegramPolicyRetryDelivery, Delay: retryAfter}, true
		}
	}
	return TelegramPolicy{}, false
}

func pollBackoff(failures int) time.Duration {
	delays := []time.Duration{time.Second, 5 * time.Second, 30 * time.Second, time.Minute}
	if failures < 1 {
		failures = 1
	}
	if failures > len(delays) {
		failures = len(delays)
	}
	return delays[failures-1]
}

func knownTelegramErrorKind(kind TelegramErrorKind) bool {
	for _, candidate := range TelegramErrorKinds() {
		if kind == candidate {
			return true
		}
	}
	return false
}
