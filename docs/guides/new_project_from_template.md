# Starting a new project from this template

The template lives at <https://github.com/t4l3x/go-webapp-template>, with
the Go module path `github.com/t4l3x/go-webapp-template`. Every project
created from it must get its own module path. Never keep the template's
path, and never switch to relative imports.

## A. GitHub template (recommended)

1. On GitHub, click **Use this template → Create a new repository**.
2. Create the new repository, e.g. `acme/payment-service`.
3. Clone it:

   ```sh
   git clone git@github.com:acme/payment-service.git
   cd payment-service
   ```

4. Initialize it:

   ```sh
   ./scripts/init-template.sh github.com/acme/payment-service payment-service
   ```

5. Copy the environment file. Its dev-only defaults work unchanged:

   ```sh
   cp .env.example .env
   ```

6. Start the stack and apply migrations:

   ```sh
   make up && make migrate-up
   ```

7. Review `git diff`, then commit.

The **Use this template** button only exists once the template
repository's owner has enabled **Settings → General → Template
repository** on GitHub. Nothing in this codebase can turn that on. If
the button is missing, use flow B.

## B. Manual clone

```sh
git clone git@github.com:t4l3x/go-webapp-template.git payment-service
cd payment-service
git remote set-url origin git@github.com:acme/payment-service.git
# or start with a fresh history: rm -rf .git && git init
./scripts/init-template.sh github.com/acme/payment-service payment-service
cp .env.example .env
make up && make migrate-up
```

## What the script does

```
./scripts/init-template.sh <new-module-path> [project-name]
```

The script first validates its inputs and refuses to run with the
template defaults. It then works on the text files that git tracks or
would track. That leaves out `.git`, ignored files such as `.env`, and
binaries. It makes these changes:

| Template value | Becomes (for `payment-service`) | Used in |
| --- | --- | --- |
| `github.com/t4l3x/go-webapp-template` | your module path | `go.mod`, imports, `tools/go.mod`, lint/goimports prefix, docs |
| `go-webapp-template` | `payment-service` | Compose project and container names, JWT issuer, mail domain |
| `go_webapp_template` | `payment_service` | Postgres database names |
| `Go Webapp Template` | `Payment Service` | README title, OpenAPI title, email copy |

It also removes content that only makes sense in the template:

- the `<!-- template:start/end -->` block in `README.md`
- the `# template:start/end` blocks in the `Makefile`
- the template smoke test (`scripts/template-check.sh`, `make/template.mk`)

It leaves generic values alone, such as process names (`api`/`worker`),
ports, TTLs and limits.

Finally it runs `go mod tidy` for the root and `tools/` modules, `gofmt`,
`go build ./...` and `go test ./...`. It exits non-zero if any of them
fails. Its last output lists what you still need to customize by hand:

- README intro and the OpenAPI description
- email wording
- a real `MAIL_FROM` domain and `APP_PUBLIC_URL`
- production secrets
- a LICENSE

If `project-name` is omitted, the script derives it from the module
path's last element. A `/vN` suffix is ignored, so
`github.com/acme/payment-service/v2` gives `payment-service`.

The script never rewrites itself or this guide. You can delete both once
the project is initialized.

## Checking the script (template maintainers)

```sh
make template-check
```

This copies the repository into a temporary directory and initializes
the copy as `github.com/acme/example-service`. It then asserts that no
template identity remains and that the result passes
`go mod tidy -diff`, `go vet` and `go test`. Your working tree is never
modified, and no Docker is needed.

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
