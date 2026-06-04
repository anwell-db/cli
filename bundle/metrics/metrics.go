package metrics

const (
	ExperimentalPythonWheelWrapperIsSet = "python_wheel_wrapper_is_set"
	ArtifactDynamicVersionIsSet         = "artifact_dynamic_version_is_set"
	ArtifactBuildCommandIsSet           = "artifact_build_command_is_set"
	ArtifactFilesIsSet                  = "artifact_files_is_set"
	PresetsNamePrefixIsSet              = "presets_name_prefix_is_set"
	AppLifecycleStarted                 = "app_lifecycle_started"
	ClusterLifecycleStarted             = "cluster_lifecycle_started"
	SqlWarehouseLifecycleStarted        = "sql_warehouse_lifecycle_started"
	SelectUsed                          = "select_used"

	// Whether workspace.state_path is under /Workspace/Shared.
	StatePathIsShared = "state_path_is_shared"

	// Whether this deploy is compatible with an automatic DMS migration. A deploy is
	// compatible when, after migration, the deployment state folder can be managed by
	// only the declared permissions — plus the deployer, but only when the state is
	// under the deployer's own home directory (/Workspace/Users/<deployer>), where the
	// deployer owns and retains access. So undeclared access by the deployer is fine
	// only there; undeclared access by anyone else is never fine. A state folder in
	// /Workspace/Shared is compatible only if group_name: users has CAN_MANAGE.
	//
	// Exactly one of the three keys below is recorded per deploy:
	//   - definitely: we observed the folder's permissions and they are compatible.
	//   - not:        we observed the folder's permissions and they are not compatible.
	//   - maybe:      no permissions section is set, so we did not sync the folder and
	//                 cannot tell without an additional GetPermissions call.
	//
	// A deployment (a bundle target, identified by deployment ID) is auto-migratable
	// only if all of its deploys are compatible, so aggregate these by deployment ID.
	IsDefinitelyAutoMigrationCompatible = "is_definitely_auto_migration_compatible"
	IsMaybeAutoMigrationCompatible      = "is_maybe_auto_migration_compatible"
	IsNotAutoMigrationCompatible        = "is_not_auto_migration_compatible"
)
