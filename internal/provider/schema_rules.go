package provider

import (
	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/listvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/mapvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/setvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
)

// withSchemaRules adds the validation every resource argument gets, at any
// depth, so configurations fail early with a clear message instead of
// reaching the CLI:
//   - collections must not contain null elements;
//   - an optional string without its own validators must not be empty
//     (null leaves it unset);
//   - an optional number without its own validators must not be negative.
//
// TestSchemaRules checks that every resource applies them.
func withSchemaRules(attrs map[string]schema.Attribute) map[string]schema.Attribute {
	for name, a := range attrs {
		if !a.IsOptional() && !a.IsRequired() {
			continue
		}
		switch a := a.(type) {
		case schema.StringAttribute:
			if len(a.Validators) == 0 {
				a.Validators = []validator.String{stringvalidator.LengthAtLeast(1)}
			}
			attrs[name] = a
		case schema.Int64Attribute:
			if len(a.Validators) == 0 {
				a.Validators = []validator.Int64{int64validator.AtLeast(0)}
			}
			attrs[name] = a
		case schema.ListAttribute:
			a.Validators = append(a.Validators, listvalidator.NoNullValues())
			attrs[name] = a
		case schema.SetAttribute:
			a.Validators = append(a.Validators, setvalidator.NoNullValues())
			attrs[name] = a
		case schema.MapAttribute:
			a.Validators = append(a.Validators, mapvalidator.NoNullValues())
			attrs[name] = a
		case schema.ListNestedAttribute:
			a.Validators = append(a.Validators, listvalidator.NoNullValues())
			a.NestedObject.Attributes = withSchemaRules(a.NestedObject.Attributes)
			attrs[name] = a
		case schema.SingleNestedAttribute:
			a.Attributes = withSchemaRules(a.Attributes)
			attrs[name] = a
		}
	}
	return attrs
}
