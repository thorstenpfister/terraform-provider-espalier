package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// An empty list, set or map in a configuration means the same as leaving it
// out: the runtime reports both as nothing. So does an object, e.g. dns,
// whose attributes are all unset or empty. Modules pass empty collections
// through, so the provider keeps them as written and does not replace a
// resource for the difference.

// keepEmptyCollections keeps collections that were written empty (in prior,
// the plan or the previous state) where the runtime now reports none, at
// any depth. Call it after setting the state.
func keepEmptyCollections(ctx context.Context, st *tfsdk.State, prior any, diags *diag.Diagnostics) {
	written := tfsdk.State{Schema: st.Schema}
	if d := written.Set(ctx, prior); d.HasError() {
		diags.Append(d...)
		return
	}
	kept, err := tftypes.Transform(st.Raw, func(p *tftypes.AttributePath, v tftypes.Value) (tftypes.Value, error) {
		if !v.IsNull() || !isCollection(v.Type()) && !v.Type().Is(tftypes.Object{}) {
			return v, nil
		}
		w, _, err := tftypes.WalkAttributePath(written.Raw, p)
		if wv, ok := w.(tftypes.Value); err == nil && ok && isEmpty(wv) {
			return wv, nil
		}
		return v, nil
	})
	if err != nil {
		diags.AddError("Keeping empty collections failed", err.Error())
		return
	}
	st.Raw = kept
}

func isCollection(t tftypes.Type) bool {
	return t.Is(tftypes.List{}) || t.Is(tftypes.Set{}) || t.Is(tftypes.Map{})
}

// isEmpty reports whether v is a known empty collection, or an object whose
// attributes are all null or empty.
func isEmpty(v tftypes.Value) bool {
	if !v.IsKnown() || v.IsNull() {
		return false
	}
	switch t := v.Type(); {
	case t.Is(tftypes.Map{}):
		var m map[string]tftypes.Value
		return v.As(&m) == nil && len(m) == 0
	case t.Is(tftypes.List{}), t.Is(tftypes.Set{}):
		var l []tftypes.Value
		return v.As(&l) == nil && len(l) == 0
	case t.Is(tftypes.Object{}):
		var attrs map[string]tftypes.Value
		if v.As(&attrs) != nil {
			return false
		}
		for _, a := range attrs {
			if !a.IsNull() && !isEmpty(a) {
				return false
			}
		}
		return true
	}
	return false
}

// sameOrEmpty reports whether a and b are equal once empty collections, at
// any depth, count as null.
func sameOrEmpty(ctx context.Context, a, b attr.Value) bool {
	if a.Equal(b) {
		return true
	}
	ta, errA := a.ToTerraformValue(ctx)
	tb, errB := b.ToTerraformValue(ctx)
	if errA != nil || errB != nil {
		return false
	}
	return emptyAsNull(ta).Equal(emptyAsNull(tb))
}

func emptyAsNull(v tftypes.Value) tftypes.Value {
	out, err := tftypes.Transform(v, func(_ *tftypes.AttributePath, v tftypes.Value) (tftypes.Value, error) {
		if isEmpty(v) {
			return tftypes.NewValue(v.Type(), nil), nil
		}
		return v, nil
	})
	if err != nil {
		return v
	}
	return out
}

// requiresReplace replaces the resource when a collection changes, but not
// between empty and null.
type requiresReplace struct{}

func (requiresReplace) Description(context.Context) string {
	return "Changing it replaces the resource; empty and unset are the same."
}

func (r requiresReplace) MarkdownDescription(ctx context.Context) string { return r.Description(ctx) }

func (requiresReplace) replace(ctx context.Context, state, plan attr.Value, resource tfsdk.State) bool {
	return !resource.Raw.IsNull() && !plan.IsUnknown() && !sameOrEmpty(ctx, state, plan)
}

func (r requiresReplace) PlanModifyList(ctx context.Context, req planmodifier.ListRequest, resp *planmodifier.ListResponse) {
	resp.RequiresReplace = r.replace(ctx, req.StateValue, req.PlanValue, req.State)
}

func (r requiresReplace) PlanModifyMap(ctx context.Context, req planmodifier.MapRequest, resp *planmodifier.MapResponse) {
	resp.RequiresReplace = r.replace(ctx, req.StateValue, req.PlanValue, req.State)
}

var (
	replaceList = []planmodifier.List{requiresReplace{}}
	replaceMap  = []planmodifier.Map{requiresReplace{}}
)
