package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// TestSchemaRules checks the edge cases of every resource argument, at any
// depth (see withSchemaRules and empty.go):
//   - an empty string and a negative number are rejected;
//   - a null element in a collection is rejected;
//   - an empty collection is rejected where null means a default
//     (optional and computed), and accepted, and never a reason to replace,
//     where it means the same as null (optional only).
func TestSchemaRules(t *testing.T) {
	ctx := context.Background()
	for _, newResource := range (&espalierProvider{}).Resources(ctx) {
		r := newResource()
		var md resource.MetadataResponse
		r.Metadata(ctx, resource.MetadataRequest{ProviderTypeName: "espalier"}, &md)
		var sr resource.SchemaResponse
		r.Schema(ctx, resource.SchemaRequest{}, &sr)
		t.Run(md.TypeName, func(t *testing.T) {
			checkSchemaRules(ctx, t, path.Empty(), sr.Schema.Attributes)
		})
	}
}

func checkSchemaRules(ctx context.Context, t *testing.T, at path.Path, attrs map[string]schema.Attribute) {
	for name, a := range attrs {
		p := at.AtName(name)
		if !a.IsOptional() && !a.IsRequired() {
			continue
		}
		switch a := a.(type) {
		case schema.StringAttribute:
			if !stringRejects(ctx, a.Validators, p, types.StringValue("")) {
				t.Errorf("%s: accepts an empty string", p)
			}
		case schema.Int64Attribute:
			if !int64Rejects(ctx, a.Validators, p, types.Int64Value(-1)) {
				t.Errorf("%s: accepts a negative number", p)
			}
		case schema.ListAttribute:
			checkCollection(ctx, t, p, a, a.ElementType, a.Validators, a.PlanModifiers)
		case schema.SetAttribute:
			checkCollection(ctx, t, p, a, a.ElementType, a.Validators, a.PlanModifiers)
		case schema.MapAttribute:
			checkCollection(ctx, t, p, a, a.ElementType, a.Validators, a.PlanModifiers)
		case schema.ListNestedAttribute:
			checkCollection(ctx, t, p, a, a.NestedObject.Type(), a.Validators, a.PlanModifiers)
			checkSchemaRules(ctx, t, p.AtListIndex(0), a.NestedObject.Attributes)
		case schema.SingleNestedAttribute:
			checkSchemaRules(ctx, t, p, a.Attributes)
		}
	}
}

// checkCollection checks a list, set or map attribute; validators and
// modifiers are the attribute's, of the matching kind.
func checkCollection[V any, M any](ctx context.Context, t *testing.T, p path.Path, a schema.Attribute, elem attr.Type, validators []V, modifiers []M) {
	t.Helper()
	null, err := elem.ValueFromTerraform(ctx, tftypes.NewValue(elem.TerraformType(ctx), nil))
	if err != nil {
		t.Fatal(err)
	}
	withNull, empty := collectionValues(a, elem, null)
	if !collectionRejects(ctx, validators, p, withNull) {
		t.Errorf("%s: accepts a null element", p)
	}
	emptyRejected := collectionRejects(ctx, validators, p, empty)
	switch {
	case a.IsComputed() && !emptyRejected:
		t.Errorf("%s: accepts an empty collection, though null means the default", p)
	case a.IsRequired() && !emptyRejected:
		t.Errorf("%s: accepts an empty collection, though it is required", p)
	case a.IsOptional() && !a.IsComputed() && emptyRejected:
		t.Errorf("%s: rejects an empty collection, which means the same as null", p)
	}
	if a.IsComputed() || a.IsRequired() {
		return
	}
	for _, m := range modifiers {
		if _, ok := any(m).(requiresReplace); !ok && isReplaceModifier(ctx, m) {
			t.Errorf("%s: replaces the resource between empty and null; use replaceList or replaceMap", p)
		}
	}
}

func collectionValues(a schema.Attribute, elem attr.Type, null attr.Value) (withNull, empty attr.Value) {
	switch a.(type) {
	case schema.SetAttribute:
		return types.SetValueMust(elem, []attr.Value{null}), types.SetValueMust(elem, nil)
	case schema.MapAttribute:
		return types.MapValueMust(elem, map[string]attr.Value{"k": null}), types.MapValueMust(elem, nil)
	default:
		return types.ListValueMust(elem, []attr.Value{null}), types.ListValueMust(elem, nil)
	}
}

// isReplaceModifier tells the framework's RequiresReplace modifiers by
// their description, as they are unexported types.
func isReplaceModifier(ctx context.Context, m any) bool {
	d, ok := m.(interface{ Description(context.Context) string })
	return ok && d.Description(ctx) == "If the value of this attribute changes, Terraform will destroy and recreate the resource."
}

func stringRejects(ctx context.Context, vs []validator.String, p path.Path, v types.String) bool {
	for _, val := range vs {
		var resp validator.StringResponse
		val.ValidateString(ctx, validator.StringRequest{Path: p, ConfigValue: v}, &resp)
		if resp.Diagnostics.HasError() {
			return true
		}
	}
	return false
}

func int64Rejects(ctx context.Context, vs []validator.Int64, p path.Path, v types.Int64) bool {
	for _, val := range vs {
		var resp validator.Int64Response
		val.ValidateInt64(ctx, validator.Int64Request{Path: p, ConfigValue: v}, &resp)
		if resp.Diagnostics.HasError() {
			return true
		}
	}
	return false
}

func collectionRejects[V any](ctx context.Context, vs []V, p path.Path, v attr.Value) bool {
	for _, val := range vs {
		var diags interface{ HasError() bool }
		switch val := any(val).(type) {
		case validator.List:
			var resp validator.ListResponse
			val.ValidateList(ctx, validator.ListRequest{Path: p, ConfigValue: v.(types.List)}, &resp)
			diags = resp.Diagnostics
		case validator.Set:
			var resp validator.SetResponse
			val.ValidateSet(ctx, validator.SetRequest{Path: p, ConfigValue: v.(types.Set)}, &resp)
			diags = resp.Diagnostics
		case validator.Map:
			var resp validator.MapResponse
			val.ValidateMap(ctx, validator.MapRequest{Path: p, ConfigValue: v.(types.Map)}, &resp)
			diags = resp.Diagnostics
		}
		if diags != nil && diags.HasError() {
			return true
		}
	}
	return false
}
