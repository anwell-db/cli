package permissions

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/databricks/cli/bundle"
	"github.com/databricks/cli/bundle/config/resources"
	"github.com/databricks/cli/bundle/libraries"
	"github.com/databricks/cli/bundle/metrics"
	"github.com/databricks/cli/bundle/paths"
	"github.com/databricks/cli/libs/diag"
	"github.com/databricks/cli/libs/iamutil"
	"github.com/databricks/databricks-sdk-go/service/workspace"
	"golang.org/x/sync/errgroup"
)

type workspaceRootPermissions struct{}

func ApplyWorkspaceRootPermissions() bundle.Mutator {
	return &workspaceRootPermissions{}
}

func (*workspaceRootPermissions) Name() string {
	return "ApplyWorkspaceRootPermissions"
}

// Apply implements bundle.Mutator.
func (*workspaceRootPermissions) Apply(ctx context.Context, b *bundle.Bundle) diag.Diagnostics {
	stateFolderPermissions, err := giveAccessForWorkspaceRoot(ctx, b)
	if err != nil {
		return diag.FromErr(err)
	}

	recordPermissionMetrics(b, stateFolderPermissions)
	return nil
}

// giveAccessForWorkspaceRoot applies the bundle's top-level permissions to the
// workspace folders and returns the resulting permissions of the folder that holds
// the deployment state, or nil when no permissions are declared or that folder is in
// /Workspace/Shared (which is not synced).
func giveAccessForWorkspaceRoot(ctx context.Context, b *bundle.Bundle) (*WorkspacePathPermissions, error) {
	var permissions []workspace.WorkspaceObjectAccessControlRequest
	for _, p := range b.Config.Permissions {
		level, err := GetWorkspaceObjectPermissionLevel(string(p.Level))
		if err != nil {
			return nil, err
		}

		permissions = append(permissions, workspace.WorkspaceObjectAccessControlRequest{
			GroupName:            p.GroupName,
			UserName:             p.UserName,
			ServicePrincipalName: p.ServicePrincipalName,
			PermissionLevel:      level,
		})
	}

	if len(permissions) == 0 {
		return nil, nil
	}

	w := b.WorkspaceClient(ctx).Workspace
	bundlePaths := paths.CollectUniqueWorkspacePathPrefixes(b.Config.Workspace)

	// Each goroutine writes the folder's resulting permissions into its own slot,
	// so they are inspected after Wait rather than concurrently.
	folderPermissions := make([]*WorkspacePathPermissions, len(bundlePaths))
	g, ctx := errgroup.WithContext(ctx)
	for i, p := range bundlePaths {
		g.Go(func() error {
			wp, err := setPermissions(ctx, w, p, permissions)
			folderPermissions[i] = wp
			return err
		})
	}

	if err := g.Wait(); err != nil {
		return nil, err
	}

	// The deployment state lives under root_path by default, or in its own folder when
	// state_path is configured outside root_path. Return that folder's permissions.
	var stateFolder string
	if pathContains(b.Config.Workspace.RootPath, b.Config.Workspace.StatePath) {
		stateFolder = b.Config.Workspace.RootPath
	} else {
		stateFolder = b.Config.Workspace.StatePath
	}

	i := slices.Index(bundlePaths, stateFolder)
	if i < 0 {
		return nil, nil
	}
	return folderPermissions[i], nil
}

// pathContains reports whether the workspace folder at parent is, or is an ancestor
// of, child. Empty paths are treated as a match because workspace paths are fully
// defaulted before deploy. Both paths are /Workspace-normalized by PrependWorkspacePrefix.
func pathContains(parent, child string) bool {
	if parent == "" || child == "" {
		return true
	}
	if child == parent {
		return true
	}
	if !strings.HasSuffix(parent, "/") {
		parent += "/"
	}
	return strings.HasPrefix(child, parent)
}

func setPermissions(ctx context.Context, w workspace.WorkspaceInterface, path string, permissions []workspace.WorkspaceObjectAccessControlRequest) (*WorkspacePathPermissions, error) {
	// If the folder is shared, then we don't need to set permissions since it's always set for all users and it's checked in mutators before.
	if libraries.IsWorkspaceSharedPath(path) {
		return nil, nil
	}

	obj, err := w.GetStatusByPath(ctx, path) //nolint:staticcheck // Deprecated in SDK v0.127.0. Migration to WorkspaceHierarchyService tracked separately.
	if err != nil {
		return nil, err
	}

	// Reusing the SetPermissions response (the folder's resulting ACL) lets us compare
	// it against the declaration without an extra API call. The Set replaces the direct
	// ACL with the declared permissions, so any principal still showing higher access is
	// inherited from a parent folder.
	resp, err := w.SetPermissions(ctx, workspace.WorkspaceObjectPermissionsRequest{
		WorkspaceObjectId:   strconv.FormatInt(obj.ObjectId, 10),
		WorkspaceObjectType: "directories",
		AccessControlList:   permissions,
	})
	if err != nil {
		return nil, err
	}

	return ObjectAclToResourcePermissions(path, resp.AccessControlList), nil
}

func GetWorkspaceObjectPermissionLevel(bundlePermission string) (workspace.WorkspaceObjectPermissionLevel, error) {
	switch bundlePermission {
	case CAN_MANAGE:
		return workspace.WorkspaceObjectPermissionLevelCanManage, nil
	case CAN_RUN:
		return workspace.WorkspaceObjectPermissionLevelCanRun, nil
	case CAN_VIEW:
		return workspace.WorkspaceObjectPermissionLevelCanRead, nil
	default:
		return "", fmt.Errorf("unsupported bundle permission level %s", bundlePermission)
	}
}

// recordPermissionMetrics records telemetry describing how the deployment state
// folder's permissions relate to the bundle's declared permissions. stateFolderPerms
// is the folder's live ACL, or nil when it was not observed (no permissions declared,
// or the folder is in /Workspace/Shared).
func recordPermissionMetrics(b *bundle.Bundle, stateFolderPerms *WorkspacePathPermissions) {
	b.Metrics.SetBoolValue(metrics.StatePathIsShared, libraries.IsWorkspaceSharedPath(b.Config.Workspace.StatePath))
	// Emit exactly one of the three auto-migration verdict keys.
	b.Metrics.SetBoolValue(autoMigrationVerdict(b, stateFolderPerms), true)
}

// autoMigrationVerdict returns the metric key describing whether the state folder is
// compatible with an automatic migration. It depends on where the state lives:
//
//   - /Workspace/Shared: readable and writable by all workspace users, so it is
//     compatible only when that broad access is declared via group_name: users
//     CAN_MANAGE. Known statically.
//   - Under another user's home (/Workspace/Users/<other>): that user owns the folder,
//     which is undeclared access we cannot account for, so it is not compatible. Known
//     statically.
//   - No permissions section, elsewhere: the folder is not synced, so its ACL is never
//     observed and we cannot tell without an additional GetPermissions call.
//   - Permissions set (non-shared): the SetPermissions response gives the live ACL.
//     The folder is compatible when every principal on it is declared, with one
//     exception: the deployer is allowed even when not declared, but only when the
//     state is under their own home (/Workspace/Users/<deployer>), where they own and
//     retain access. Elsewhere the deployer has no inherent access and the exemption
//     does not apply.
func autoMigrationVerdict(b *bundle.Bundle, stateFolderPerms *WorkspacePathPermissions) string {
	statePath := b.Config.Workspace.StatePath

	if libraries.IsWorkspaceSharedPath(statePath) {
		if usersGroupCanManage(b.Config.Permissions) {
			return metrics.IsDefinitelyAutoMigrationCompatible
		}
		return metrics.IsNotAutoMigrationCompatible
	}

	deployer := identityName(deployingUserPermission(b))
	owner := homeOwner(statePath)

	if stateFolderPerms == nil {
		// No permissions section: the folder was not synced, so its ACL is unobserved.
		if owner != "" && owner != deployer {
			// Another user owns the home the state lives under; their access is undeclared.
			return metrics.IsNotAutoMigrationCompatible
		}
		// The deployer's own home or a non-home path; we cannot tell without a GetPermissions call.
		return metrics.IsMaybeAutoMigrationCompatible
	}

	// The deployer's undeclared access is allowed only under their own home.
	allowed := b.Config.Permissions
	if deployer != "" && owner == deployer {
		allowed = append(slices.Clone(allowed), deployingUserPermission(b))
	}

	if stateFolderPerms.Exceeds(allowed) {
		return metrics.IsNotAutoMigrationCompatible
	}
	return metrics.IsDefinitelyAutoMigrationCompatible
}

// homeOwner returns the user that owns the home directory the path is under, i.e.
// <owner> for /Workspace/Users/<owner>/..., or "" if the path is not under a home.
func homeOwner(path string) string {
	const prefix = "/Workspace/Users/"
	if !strings.HasPrefix(path, prefix) {
		return ""
	}
	rest := path[len(prefix):]
	if before, _, ok := strings.Cut(rest, "/"); ok {
		return before
	}
	return rest
}

// identityName returns the user or service principal name of a permission.
func identityName(p resources.Permission) string {
	if p.UserName != "" {
		return p.UserName
	}
	return p.ServicePrincipalName
}

// deployingUserPermission returns a CAN_MANAGE permission for the deploying identity.
func deployingUserPermission(b *bundle.Bundle) resources.Permission {
	p := resources.Permission{Level: CAN_MANAGE}
	cu := b.Config.Workspace.CurrentUser
	if cu == nil {
		return p
	}
	if iamutil.IsServicePrincipal(cu.User) {
		p.ServicePrincipalName = cu.UserName
	} else {
		p.UserName = cu.UserName
	}
	return p
}

func usersGroupCanManage(perms []resources.Permission) bool {
	for _, p := range perms {
		if p.GroupName == "users" && p.Level == CAN_MANAGE {
			return true
		}
	}
	return false
}
