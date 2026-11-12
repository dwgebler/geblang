Run from this directory with Geblang on PATH:

```sh
geblang -m producer Ada
geblang worker --entry worker --once
geblang worker --entry worker stats --format json
geblang build --entry worker --out worker-app .
./worker-app --once
```

The default SQLite file is jobs.db; JOBS_DB selects another file. Run the
publisher continuously with --scheduler-only. Stop all publishers/producers/
workers before migrating a legacy_jobs table using worker migrate --dry-run,
then worker migrate --apply --stopped through the module entry point.
