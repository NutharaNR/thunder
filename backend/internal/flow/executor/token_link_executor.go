/*
 * Copyright (c) 2026, WSO2 LLC. (https://www.wso2.com).
 *
 * WSO2 LLC. licenses this file to you under the Apache License,
 * Version 2.0 (the "License"); you may not use this file except
 * in compliance with the License.
 * You may obtain a copy of the License at
 *
 * http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing,
 * software distributed under the License is distributed on an
 * "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
 * KIND, either express or implied.  See the License for the
 * specific language governing permissions and limitations
 * under the License.
 */

package executor

import (
	"fmt"
	"net/url"

	"github.com/asgardeo/thunder/internal/flow/common"
	"github.com/asgardeo/thunder/internal/flow/core"
	oauth2const "github.com/asgardeo/thunder/internal/oauth/oauth2/constants"
	"github.com/asgardeo/thunder/internal/system/config"
	"github.com/asgardeo/thunder/internal/system/log"
	"github.com/asgardeo/thunder/internal/system/utils"
)

// tokenLinkConfig holds flow-type-specific configuration for the token link executor.
type tokenLinkConfig struct {
	route           string // URL path segment, e.g. "invite" or "recovery"
	tokenInputKey   string // key used to read the token from UserInputs
	storedTokenKey  string // RuntimeData key for the stored token
	linkKey         string // RuntimeData key for the generated link
	templateDataKey string // key used inside the template data map
	exposeAsData    bool   // when true, also writes the link to AdditionalData (user-onboarding)
}

// tokenLinkExecutor generates and verifies one-time token links for invite and recovery flows.
// The behavior (route, runtime keys, template variables) is resolved from the flow type at execution time.
type tokenLinkExecutor struct {
	core.ExecutorInterface
	logger *log.Logger
}

// newTokenLinkExecutor creates a new instance of the token link executor.
func newTokenLinkExecutor(flowFactory core.FlowFactoryInterface) *tokenLinkExecutor {
	logger := log.GetLogger().With(log.String(log.LoggerKeyComponentName, "TokenLinkExecutor"))
	base := flowFactory.CreateExecutor(
		ExecutorNameTokenLinkExecutor,
		common.ExecutorTypeUtility,
		[]common.Input{},
		[]common.Input{},
	)
	return &tokenLinkExecutor{
		ExecutorInterface: base,
		logger:            logger,
	}
}

// resolveConfig returns flow-type-specific configuration.
func (e *tokenLinkExecutor) resolveConfig(ctx *core.NodeContext) (*tokenLinkConfig, error) {
	switch ctx.FlowType {
	case common.FlowTypeRegistration, common.FlowTypeUserOnboarding:
		return &tokenLinkConfig{
			route:           "invite",
			tokenInputKey:   userInputInviteToken,
			storedTokenKey:  common.RuntimeKeyStoredInviteToken,
			linkKey:         common.RuntimeKeyInviteLink,
			templateDataKey: "inviteLink",
			exposeAsData:    ctx.FlowType == common.FlowTypeUserOnboarding,
		}, nil
	case common.FlowTypeRecovery:
		return &tokenLinkConfig{
			route:           "recovery",
			tokenInputKey:   userInputRecoveryToken,
			storedTokenKey:  common.RuntimeKeyStoredRecoveryToken,
			linkKey:         common.RuntimeKeyRecoveryLink,
			templateDataKey: "recoveryLink",
		}, nil
	default:
		return nil, fmt.Errorf("unsupported flow type for TokenLinkExecutor: %s", ctx.FlowType)
	}
}

// GetExecutionPolicy returns the execution policy for the given mode.
// The verify mode skips challenge token validation because the token in the link itself serves as the challenge.
func (e *tokenLinkExecutor) GetExecutionPolicy(mode string) *core.ExecutionPolicy {
	if mode == ExecutorModeVerify {
		return &core.ExecutionPolicy{
			SkipChallengeValidation: true,
			AllowSegmentRestart:     true,
		}
	}
	return nil
}

// Execute delegates to the appropriate mode handler based on the executor mode.
func (e *tokenLinkExecutor) Execute(ctx *core.NodeContext) (*common.ExecutorResponse, error) {
	switch ctx.ExecutorMode {
	case ExecutorModeGenerate:
		return e.executeGenerate(ctx)
	case ExecutorModeVerify:
		return e.executeVerify(ctx)
	default:
		return nil, fmt.Errorf("invalid executor mode for TokenLinkExecutor: %s", ctx.ExecutorMode)
	}
}

// executeGenerate generates a token and builds the corresponding link.
func (e *tokenLinkExecutor) executeGenerate(ctx *core.NodeContext) (*common.ExecutorResponse, error) {
	logger := e.logger.With(log.String(log.LoggerKeyExecutionID, ctx.ExecutionID))
	logger.Debug("Executing token link executor in generate mode")

	cfg, err := e.resolveConfig(ctx)
	if err != nil {
		return nil, err
	}

	execResp := &common.ExecutorResponse{
		AdditionalData: make(map[string]string),
		RuntimeData:    make(map[string]string),
		ForwardedData:  make(map[string]interface{}),
	}

	token, err := e.getOrGenerateToken(ctx, cfg)
	if err != nil {
		logger.Error("Failed to get or generate token", log.Error(err))
		execResp.Status = common.ExecFailure
		execResp.FailureReason = "Failed to generate token"
		return execResp, nil
	}

	link := e.buildLink(ctx, cfg, token)

	execResp.RuntimeData[cfg.storedTokenKey] = token
	execResp.RuntimeData[cfg.linkKey] = link
	execResp.ForwardedData[common.ForwardedDataKeyTemplateData] = map[string]interface{}{
		cfg.templateDataKey: link,
	}

	if cfg.exposeAsData {
		execResp.AdditionalData[common.DataInviteLink] = link
	}

	execResp.Status = common.ExecComplete
	return execResp, nil
}

// executeVerify validates the user-provided token against the stored token.
func (e *tokenLinkExecutor) executeVerify(ctx *core.NodeContext) (*common.ExecutorResponse, error) {
	logger := e.logger.With(log.String(log.LoggerKeyExecutionID, ctx.ExecutionID))
	logger.Debug("Executing token link executor in verify mode")

	cfg, err := e.resolveConfig(ctx)
	if err != nil {
		return nil, err
	}

	execResp := &common.ExecutorResponse{
		AdditionalData: make(map[string]string),
		RuntimeData:    make(map[string]string),
	}

	tokenInput := ctx.UserInputs[cfg.tokenInputKey]
	if tokenInput == "" {
		execResp.Status = common.ExecUserInputRequired
		return execResp, nil
	}

	storedToken, hasStoredToken := ctx.RuntimeData[cfg.storedTokenKey]
	if !hasStoredToken || storedToken == "" {
		logger.Debug("No token found in runtime data")
		execResp.Status = common.ExecFailure
		execResp.FailureReason = "Invalid token"
		return execResp, nil
	}

	if tokenInput != storedToken {
		logger.Debug("Token mismatch")
		execResp.Status = common.ExecFailure
		execResp.FailureReason = "Invalid token"
		return execResp, nil
	}

	logger.Debug("Token validated successfully")
	execResp.Status = common.ExecComplete
	return execResp, nil
}

// getOrGenerateToken reuses an existing stored token or generates a new one.
func (e *tokenLinkExecutor) getOrGenerateToken(ctx *core.NodeContext, cfg *tokenLinkConfig) (string, error) {
	if storedToken, exists := ctx.RuntimeData[cfg.storedTokenKey]; exists && storedToken != "" {
		return storedToken, nil
	}
	return utils.GenerateUUIDv7()
}

// buildLink constructs the token link using the GateClient configuration.
func (e *tokenLinkExecutor) buildLink(ctx *core.NodeContext, cfg *tokenLinkConfig, token string) string {
	gateConfig := config.GetServerRuntime().Config.GateClient
	gateAppURL := fmt.Sprintf("%s://%s:%d%s",
		gateConfig.Scheme,
		gateConfig.Hostname,
		gateConfig.Port,
		gateConfig.Path)
	queryParams := url.Values{
		"executionId":     []string{ctx.ExecutionID},
		cfg.tokenInputKey: []string{token},
	}
	if ctx.AppID != "" {
		queryParams.Set(oauth2const.AppID, ctx.AppID)
	}
	return fmt.Sprintf("%s/%s?%s", gateAppURL, cfg.route, queryParams.Encode())
}
