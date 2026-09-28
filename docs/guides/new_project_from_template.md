# Starting a new project from this template

The template lives at <https://github.com/t4l3x/go-webapp-template> with
the Go module path `github.com/t4l3x/go-webapp-template`. Every project
created from it must get its own module path. Never keep the template's
path, and never switch to relative imports.

## A. GitHub template (recommended)

1. On GitHub, click **Use this template → Create a new repository**.
2. Clone your new repository and initialize it:

   ```sh
   git clone git@github.com:acme/payment-service.git
   cd payment-service
   ./scripts/init-template.sh github.com/acme/payment-service payment-service
   ```

3. Review `git diff`, then commit.

## B. Manual clone

```sh
git clone git@github.com:t4l3x/go-webapp-template.git payment-service
cd payment-service
git remote set-url origin git@github.com:acme/payment-service.git
# or start with a fresh history: rm -rf .git && git init
./scripts/init-template.sh github.com/acme/payment-service payment-service
```

## What the script does

```
./scripts/init-template.sh <new-module-path> [project-name]
```

It validates its inputs and refuses to run with the template defaults.
It then rewrites the text files that git tracks or would track, which
leaves out `.git`, ignored files such as `.env`, and binaries. These are
the substitutions:

| Template value | Becomes (for `payment-service`) | Used in |
| --- | --- | --- |
| `github.com/t4l3x/go-webapp-template` | your module path | `go.mod`, imports, `tools/go.mod`, lint/goimports prefix, docs |
| `go-webapp-template` | `payment-service` | Compose project and container names, JWT issuer, mail domain |
| `go_webapp_template` | `payment_service` | Postgres database names |
| `Go Webapp Template` | `Payment Service` | OpenAPI title, email copy, README |

It then runs `go mod tidy` for the root and `tools/` modules, `gofmt`,
`go build ./...` and `go test ./...`, and exits non-zero if any of them
fails.

If `project-name` is omitted, the script derives it from the module
path's last element. A `/vN` suffix is ignored, so
`github.com/acme/payment-service/v2` gives `payment-service`.

The script and this guide describe the template itself. The script never
rewrites them, and you can delete both once the project is initialized.

## Alternative: `gonew`

[`gonew`](https://pkg.go.dev/golang.org/x/tools/cmd/gonew) can copy the
template and rewrite the module path:

```sh
go run golang.org/x/tools/cmd/gonew@latest github.com/t4l3x/go-webapp-template github.com/acme/payment-service
```

`gonew` rewrites only Go code and `go.mod`. The Compose and container
names, the lint prefix, `tools/go.mod` and the docs keep the template's
values. Run `./scripts/init-template.sh` afterwards to finish the job: it
reads the current module path from `go.mod`, so it works on a
`gonew`-renamed copy too.
