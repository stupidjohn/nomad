package testutil

import (
	"fmt"

	hcl1 "github.com/hashicorp/hcl"

	"github.com/hashicorp/hcl2/hcl"
	"github.com/hashicorp/nomad/helper/uuid"
	"github.com/hashicorp/nomad/plugins/drivers"
	"github.com/hashicorp/nomad/plugins/shared"
	"github.com/hashicorp/nomad/plugins/shared/hclspec"
)

// ParseTaskConfig parses task hcl, validates it against driver schema, and deserializes it
// into task config struct.  If config is invalid, it returns the diagnostics hcl library reports.
//
//  It uses the same config pipeline used by nomad agent but more lightweight for testing purposes.
//  Currently, the function does not support attribute/environment interpolation.
func ParseTaskConfig(taskHcl string, configSchema *hclspec.Spec, v interface{}) (error, hcl.Diagnostics) {
	var err error

	////// Prepare to operate on schema
	configSchemaDecl, diag := hclspec.Convert(configSchema)
	if diag.HasErrors() {
		return fmt.Errorf("failed to convert schema: %v", diag.Error()), nil
	}

	////// Parse and validate
	// We must parse config as hcl1 first then again as hcl2 with schema validation.
	// Nomad parses job description as hcl1, then interpret the task/driver config
	// specifically yet again as hcl2
	hcl1Parsed := map[string]interface{}{}
	err = hcl1.Decode(&hcl1Parsed, taskHcl)
	if err != nil {
		return fmt.Errorf("failed to decode config as hcl1: %v", err), nil
	}

	config, ok := hcl1Parsed["config"]
	if !ok {
		return fmt.Errorf(`expected a "config" stanza, but found none`), nil
	}

	// Parse task config as hcl2 and validate against schema
	evalCtx := &hcl.EvalContext{
		Functions: shared.GetStdlibFuncs(),
	}

	val, diag := shared.ParseHclInterface(config, configSchemaDecl, evalCtx)
	if diag.HasErrors() {
		return fmt.Errorf("failed to parse config: %s", diag.Error()), diag
	}

	////// Serializes to MsgPack and back to stay truthful to implementation
	task := &drivers.TaskConfig{
		ID:   uuid.Generate(),
		Name: "test",
	}
	if err = task.EncodeDriverConfig(val); err != nil {
		return fmt.Errorf("failed to encode driver config: %v", err), nil
	}

	err = task.DecodeDriverConfig(&v)
	if err != nil {
		return fmt.Errorf("failed to decode driver config: %v", err), nil
	}

	return nil, nil
}
