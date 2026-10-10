# Phase 1A — Prerequisites: Tools, AWS Account, GitHub

> **Goal:** every tool installed and verified, an AWS account that is secured and budget‑guarded, a CLI login that uses short‑lived credentials, and a GitHub account ready for the repo.
> **Time:** 60–90 min (most of it is downloads and AWS sign‑up).
> **Run all commands in:** the macOS **Terminal** app (Applications → Utilities → Terminal), unless a step says otherwise. Commands are for **zsh**, the default macOS shell.

---

## Step 0 — Find out what kind of Mac you have

```bash
uname -m
```

- `arm64` → Apple Silicon (M1/M2/M3/M4). This guide's defaults match you.
- `x86_64` → Intel Mac. Everything still works; tell me and I'll flag the few differences.

---

## Step 1 — Homebrew (the macOS package manager)

**What/why:** Homebrew installs and updates developer tools with one command, so we don't download installers by hand.

```bash
# Is it already installed?
brew --version
```

If you see `command not found`, install it:

```bash
/bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)"
```

It asks for your Mac password (typing shows nothing — that's normal) and may install the Xcode Command Line Tools (a few minutes). **At the end it prints "Next steps"** — on Apple Silicon, run them; they look like this:

```bash
echo 'eval "$(/opt/homebrew/bin/brew shellenv)"' >> ~/.zprofile
eval "$(/opt/homebrew/bin/brew shellenv)"
```

✅ **Verify:** `brew --version` → `Homebrew 4.x.x` (or newer).

---

## Step 2 — Install the command‑line tools

Copy this whole block into Terminal:

```bash
brew update

# Core languages & tools
brew install go git jq

# Cloud & Kubernetes
brew install awscli kubectl helm

# Terraform comes from HashiCorp's own "tap" (Homebrew core stopped shipping new versions)
brew tap hashicorp/tap
brew install hashicorp/tap/terraform

# Go quality & database tooling
brew install golangci-lint golang-migrate

# Database / cache clients (we only use the CLIs, we won't run these servers on the Mac)
brew install libpq redis

# Security scanning, load testing, GitHub CLI
brew install trivy hey gh
```

What each one is for:

| Tool | Used for | Phase |
|---|---|---|
| `go` | Compile and test the API | 1+ |
| `git` | Version control | 1+ |
| `jq` | Pretty‑print / query JSON in the terminal | all |
| `awscli` (`aws`) | Talk to AWS from the terminal | 7+ |
| `kubectl` | Talk to Kubernetes | 8+ |
| `helm` | Install packaged K8s apps (LB controller, External Secrets) | 8 |
| `terraform` | Infrastructure as code | 7 |
| `golangci-lint` | Runs ~dozens of Go linters at once | 5, 10 |
| `golang-migrate` (`migrate`) | Apply/rollback SQL migrations | 2 |
| `libpq` (`psql`) | Postgres command‑line client | 2 |
| `redis` (`redis-cli`) | Redis command‑line client | 4 |
| `trivy` | Scan Docker images for vulnerabilities | 6, 10 |
| `hey` | Generate HTTP load to trigger autoscaling | 9 |
| `gh` | GitHub from the terminal | 10 |

### 2a. Put `psql` and Go‑installed tools on your PATH

`libpq` is "keg‑only" (Homebrew doesn't link it automatically), and tools installed with `go install` land in `~/go/bin`. Add both to your shell profile:

```bash
echo 'export PATH="$(brew --prefix libpq)/bin:$PATH"' >> ~/.zshrc
echo 'export PATH="$PATH:$HOME/go/bin"' >> ~/.zshrc
source ~/.zshrc
```

### 2b. Go security checker

```bash
go install golang.org/x/vuln/cmd/govulncheck@latest
```

✅ **Verify:**

```bash
go version            # go version go1.2x.x darwin/arm64
terraform version     # Terraform v1.10.0 or newer  (we need ≥ 1.10 for S3 state locking)
aws --version         # aws-cli/2.x.x
kubectl version --client
helm version
psql --version        # psql (PostgreSQL) 1x.x
redis-cli --version
migrate -version
golangci-lint --version
govulncheck -version
trivy --version
```

---

## Step 3 — Docker Desktop

**What/why:** Docker runs containers. On a Mac it runs inside a small Linux VM that Docker Desktop manages for you. We use it for local Postgres/Redis (Phase 2/4) and to build the API image (Phase 6).

```bash
brew install --cask docker
```

1. Open **Docker** from Applications (or `open -a Docker`).
2. Accept the license, skip/sign in (sign‑in is optional), wait until the whale icon in the menu bar stops animating and says **"Docker Desktop is running"**.
3. Click the whale → **Settings** (gear) → **Resources** → set **Memory to at least 4 GB** → **Apply & restart**.

> Docker Desktop is free for personal use and small businesses. (Alternatives such as Colima exist; stick with Docker Desktop for this guide.)

✅ **Verify:**

```bash
docker version             # shows both Client and Server sections
docker run --rm hello-world
```

Expected: `Hello from Docker! This message shows that your installation appears to be working correctly.`

---

## Step 4 — Editor and Postman

```bash
brew install --cask visual-studio-code postman
```

1. Open **VS Code** → left sidebar **Extensions** (four squares icon) → search and install:
   - **Go** (publisher: Go Team at Google). When it prompts "Install all" for Go tools — click it.
   - **HashiCorp Terraform**
   - **YAML** (Red Hat)
   - **Docker** (Microsoft)
2. Open **Postman** → create a free account (or "Continue without an account" for now — an account lets you sync the collection we build).

---

## Step 5 — AWS account (secure it before anything else)

### 5.1 Create the account
1. Go to <https://aws.amazon.com> → **Create an AWS Account**.
2. Use your email, choose an account name like `devi-learning`, complete identity (card + phone) and choose the **Basic support – Free** plan.

### 5.2 Protect the root user with MFA
The *root user* (your sign‑up email) can do anything, including close the account. We lock it down and never use it day‑to‑day.

1. Sign in at <https://console.aws.amazon.com> as **Root user**.
2. Top‑right, click your **account name** → **Security credentials**.
3. Under **Multi‑factor authentication (MFA)** → **Assign MFA device** → name `root-mfa` → choose **Passkey or security key** (Touch ID works) or **Authenticator app** → follow the prompts.

✅ Sign out and back in — you should be asked for the MFA.

### 5.3 Create budget alerts (before creating any resource)
1. In the top search bar type **Billing and Cost Management** → open it.
2. Left menu → **Budgets** → **Create budget**.
3. Choose **Use a template (simplified)** → template **Zero spend budget** → enter your email → **Create budget**. (Emails you on the first cent spent.)
4. **Create budget** again → **Monthly cost budget** → **Budgeted amount: 30** (USD) → your email → **Create budget**. (Emails at 85 % and 100 % of $30.)

### 5.4 Pick the region
In the top‑right region selector choose **US East (N. Virginia) us-east-1**. Do this every time you open the console — resources in another region are invisible in this one, which is the #1 "where did my cluster go?" confusion.

### 5.5 Create your everyday admin user with IAM Identity Center
**Why not an IAM user with access keys?** Access keys are long‑lived passwords that sit in a file on your disk forever. Identity Center gives you **temporary credentials that expire after a few hours** — the modern AWS recommendation.

1. Search **IAM Identity Center** → **Enable**. If asked, choose **Enable with AWS Organizations** (recommended) → **Continue**.
2. Left menu → **Users** → **Add user**:
   - Username: `devi`
   - Email: your email; first/last name
   - Password: **Send an email to this user with password setup instructions**
   - **Next** → (skip groups) **Next** → **Add user**.
3. Open the email "Invitation to join AWS IAM Identity Center" → **Accept invitation** → set a password → register an MFA device when prompted.
4. Back in the console: **Multi-account permissions → Permission sets** → **Create permission set** → **Predefined permission set** → **AdministratorAccess** → **Next** → Session duration **8 hours** → **Next** → **Create**.
5. **Multi-account permissions → AWS accounts** → tick your account → **Assign users or groups** → **Users** tab → tick `devi` → **Next** → tick **AdministratorAccess** → **Next** → **Submit**.
6. Left menu → **Dashboard** → copy the **AWS access portal URL** (looks like `https://d-xxxxxxxxxx.awsapps.com/start`).

> Admin access is fine for *you* while learning. The CI pipeline will get its own narrowly‑scoped role in Phase 10 — that's where least privilege really matters.

### 5.6 Log in from the terminal (AWS CLI + SSO)

```bash
aws configure sso
```

Answer the prompts:

| Prompt | Answer |
|---|---|
| SSO session name | `achievement` |
| SSO start URL | *the portal URL you copied* |
| SSO region | `us-east-1` |
| SSO registration scopes | press **Enter** (default `sso:account:access`) |

A browser opens → **Allow access**. Back in the terminal:

| Prompt | Answer |
|---|---|
| (account / role selection) | your account, `AdministratorAccess` |
| Default client Region | `us-east-1` |
| CLI default output format | `json` |
| Profile name | `achievement` |

Make it the default for new terminal windows:

```bash
echo 'export AWS_PROFILE=achievement' >> ~/.zshrc
source ~/.zshrc
```

✅ **Verify:**

```bash
aws sts get-caller-identity
```

Expected (numbers differ):

```json
{
  "UserId": "AROA…:devi",
  "Account": "123456789012",
  "Arn": "arn:aws:sts::123456789012:assumed-role/AWSReservedSSO_AdministratorAccess_abc123/devi"
}
```

`assumed-role` = temporary credentials. 

When the session expires (after 8 h) commands fail with *"Token has expired"* → just run:

```bash
aws sso login
```

---

## Step 6 — GitHub

1. Create an account at <https://github.com> if you don't have one, and enable 2FA (**Settings → Password and authentication**).
2. Log the CLI in:

```bash
gh auth login
```
Choose **GitHub.com → HTTPS → Yes (authenticate Git) → Login with a web browser**, paste the one‑time code.

3. Tell Git who you are (used in commit history):

```bash
git config --global user.name  "Your Name"
git config --global user.email "you@example.com"
git config --global init.defaultBranch main
```

✅ **Verify:** `gh auth status` → `✓ Logged in to github.com account <you>`.

We create the repository itself in Phase 1B.

---

## Step 7 — Run the checker

From the project folder:

```bash
cd "/Users/deviprasad/All Data/Data/Personal_Projects/AchivementAPI"
bash scripts/check-prereqs.sh
```

> The quotes matter — the path contains a space (`All Data`). Without quotes, the shell splits it into two arguments and `cd` fails with *"too many arguments"* / *"no such file or directory"*.

Expected ending: `All checks passed ✅`. Anything marked ❌ tells you what to fix.

---

## Troubleshooting

| Symptom | Cause | Fix |
|---|---|---|
| `zsh: command not found: brew` right after install | Shell profile not updated | Run the two `brew shellenv` lines from Step 1, or open a new Terminal window. |
| `psql: command not found` | libpq is keg‑only | Step 2a, then `source ~/.zshrc`. |
| `govulncheck: command not found` | `~/go/bin` not on PATH | Step 2a. |
| `Cannot connect to the Docker daemon` | Docker Desktop not running | `open -a Docker`, wait for "running". |
| `terraform` version < 1.10 | Old install from Homebrew core | `brew uninstall terraform && brew install hashicorp/tap/terraform` |
| `Error loading SSO Token: Token for … does not exist` / `expired` | Not logged in or session ended | `aws sso login` |
| `Unable to locate credentials` | `AWS_PROFILE` not set in this terminal | `export AWS_PROFILE=achievement` |
| Console shows no resources you created | Wrong region selected | Switch top‑right selector to us‑east‑1. |
| Identity Center invite email never arrives | Spam folder / typo | Identity Center → Users → `devi` → **Send email verification / Reset password**. |

---

## ✅ Checkpoint 1A

You're ready for Phase 1B when **all** are true:

- [ ] `bash scripts/check-prereqs.sh` ends with *All checks passed*
- [ ] `docker run --rm hello-world` prints the greeting
- [ ] `aws sts get-caller-identity` shows an `assumed-role/AWSReservedSSO_AdministratorAccess…` ARN
- [ ] Two budgets exist (Zero spend + $30 monthly) and root has MFA
- [ ] `gh auth status` shows you logged in

**Next — Phase 1B:** create the Git repo, initialise the Go module, and get a first `/healthz` endpoint running with config loading, structured logging and graceful shutdown.
