# Project workspace

Run these commands from this directory with Geblang 1.41.0 or newer:

```sh
geblang resolve --explain report_app.main
geblang resolve --from apps/report --explain project_tasks.model
geblang install --workspace
geblang --vm-strict -m report_app.main
geblang --disable-vm -m report_app.main
geblang build --entry report_app.main --out /tmp/workspace-report
/tmp/workspace-report
```

All three executions print the contents of expected.txt. Run the VM command
again to exercise cached bytecode. The executable runs outside this checkout.
From apps/report/, the same module commands work with that member's manifest.

The root lists packages and explicitly depends on report_app. report_app
explicitly depends on project_tasks through `workspace: true`. project_tasks
uses an ordinary path dependency on external/labels, which is not a workspace
member. That package could instead be an installed Git dependency with the
same declared package name and API; workspace resolution does not rename it.

Each package keeps its source root and dependency boundaries. Listing a
workspace member alone does not expose it to the root or another member.
The example also uses ordinary sibling modules, exported classes, inheritance,
and an inherited method captured as a callable across package boundaries.
