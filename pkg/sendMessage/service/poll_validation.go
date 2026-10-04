package send_service

import (
	"fmt"
	"github.com/lucasgiovannibr/whatygo/pkg/apierror"
	"strings"
)

// maxPollOptions is the number of options WhatsApp accepts in a poll.
const maxPollOptions = 12

// ValidatePoll checks a poll request. It returns an error a caller can fix:
//
//   - more than 12 options or an empty option is rejected by WhatsApp;
//   - a repeated option makes votes ambiguous (a vote names its option by a hash);
//   - a maxAnswer above the number of options was silently turned into 0 by the
//     library, which means "any number of answers", the opposite of what was asked.
//     (0 itself is kept: it is the documented "unlimited".)
func (p *PollStruct) ValidatePoll() error {
	if strings.TrimSpace(p.Question) == "" {
		return apierror.Invalid("question is required")
	}
	if len(p.Options) < 2 {
		return apierror.Invalid("minimum 2 options are required")
	}
	if len(p.Options) > maxPollOptions {
		return fmt.Errorf("maximum %d options are allowed", maxPollOptions)
	}

	seen := make(map[string]struct{}, len(p.Options))
	for i, o := range p.Options {
		name := strings.TrimSpace(o)
		if name == "" {
			return apierror.Invalid(fmt.Sprintf("option %d is empty", i+1))
		}
		if _, dup := seen[name]; dup {
			return fmt.Errorf("option %q is repeated", name)
		}
		seen[name] = struct{}{}
	}

	if p.MaxAnswer < 0 || p.MaxAnswer > len(p.Options) {
		return apierror.Invalid(fmt.Sprintf("maxAnswer must be between 0 (unlimited) and the number of options (%d)", len(p.Options)))
	}
	return nil
}
