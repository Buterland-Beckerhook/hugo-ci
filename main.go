package main

import (
  "fmt"
  "github.com/google/go-github/v32/github"
  "github.com/robfig/cron/v3"
  "github.com/tidwall/gjson"
  "log"
  "net/http"
  "net/smtp"
  "os"
  "os/exec"
  "path"
  "strconv"
  "strings"
)

const (
  checkoutDir   = "/data"
  liveOut       = "/live"
  betaOut       = "/beta"
  sourceWebhook = "webhook"
  sourceCronJob = "cron"
)

var (
  repoUrl        = ""
  liveBranch     = ""
  liveBaseUrl    = ""
  betaBranch     = ""
  betaBaseUrl    = ""
  githubSecToken = ""
  buildRunning   = false
)

func main() {

  var err error

  repoUrl = os.Getenv("REPO_URL")
  liveBranch = os.Getenv("LIVE_BRANCH")
  liveBaseUrl = os.Getenv("LIVE_BASE_URL")
  betaBranch = os.Getenv("BETA_BRANCH")
  betaBaseUrl = os.Getenv("BETA_BASE_URL")
  githubSecToken = os.Getenv("GITHUB_SEC_TOKEN")

  fmt.Printf("REPO_URL: %s\n", repoUrl)
  fmt.Printf("LIVE_BRANCH: %s\n", liveBranch)
  fmt.Printf("BETA_BRANCH: %s\n", betaBranch)

  c := cron.New()
  _, err = c.AddFunc(os.Getenv("LIVE_BUILD_CRON"), func() {
    build(sourceCronJob, liveBranch, liveOut, liveBaseUrl, false)
  })
  if err != nil {
    fmt.Println("error adding cron job for live build", err)
  }
  _, err = c.AddFunc(os.Getenv("BETA_BUILD_CRON"), func() {
    build(sourceCronJob, betaBranch, betaOut, betaBaseUrl, true)
  })
  if err != nil {
    fmt.Println("error adding cron job for beta build", err)
  }
  defer c.Stop()
  c.Start()

  router := http.NewServeMux()
  router.HandleFunc("/webhook", webhookHandler)

  addr := os.Getenv("BIND_ADDRESS")
  fmt.Printf("starting listener: %s\n", addr)
  err = http.ListenAndServe(addr, router)
  if err != nil {
    log.Fatal(err)
  }
}
func webhookHandler(w http.ResponseWriter, r *http.Request) {
  if r.Method != http.MethodPost {
    w.WriteHeader(http.StatusMethodNotAllowed)
    return
  }
  defer r.Body.Close()
  data, err := github.ValidatePayload(r, []byte(githubSecToken))
  if err != nil {
    fmt.Printf("bad signature: %v\n", err)
    w.WriteHeader(http.StatusBadRequest)
    return
  }

  event := github.WebHookType(r)
  fmt.Printf("event: %s\n", event)

  devId := github.DeliveryID(r)
  fmt.Printf("deliveryId: %s\n", devId)

  ref := gjson.GetBytes(data, "ref")
  if ref.Exists() {
    branch := path.Base(ref.Str)
    if branch == betaBranch {
      go func() {
        build(sourceWebhook, betaBranch, betaOut, betaBaseUrl, true)
      }()
    } else if branch == liveBranch {
      build(sourceWebhook, liveBranch, liveOut, liveBaseUrl, false)
    } else {
      fmt.Printf("no branch to build: %s\n", branch)
    }
  }

  w.WriteHeader(http.StatusOK)
}

func build(source, branch, out, baseUrl string, buildDrafts bool) {

  var sb *strings.Builder
  srv := os.Getenv("MAIL_SMTP_SERVER")
  recipients := os.Getenv("MAIL_RECIPIENTS")
  allOk := false
  if srv != "" && recipients != "" {
    sb = &strings.Builder{}
  }
  defer func() {
    sendMail(sb, source, allOk, srv, recipients)
  }()

  if buildRunning {
    logf(sb, "already a build running, aborting (%s)\n", branch)
    return
  }
  buildRunning = true
  defer func() { buildRunning = false }()

  logf(sb, "\nbuilding %s...\n", branch)
  err := checkout(branch)
  if err != nil {
    logf(sb, "error during checkout: %v\n", err)
    return
  }
  fmt.Println("checkout OK")

  args := []string{"-d", out, "--baseURL", baseUrl}
  if buildDrafts {
    args = append(args, "-D")
  }
  s, err := hugo(args...)
  if err != nil {
    logf(sb, "error during build: %v\n", err)
    return
  } else {
    logf(sb, "%s\n", s)
  }
  logf(sb, "\nBuild %s\n", branch, source)
  allOk = true
}

func logf(sb *strings.Builder, format string, a ...interface{}) {
  fmt.Printf(format, a)
  if sb != nil {
    sb.WriteString(fmt.Sprintf(format, a))
  }
}

func sendMail(sb *strings.Builder, source string, ok bool, srv string, recipients string) {
  if sb == nil {
    return
  }

  if ok && source == sourceWebhook && !getBoolEnv("MAIL_PUSH_SUCCESS") {
    return
  } else if ok && source == sourceCronJob && !getBoolEnv("MAIL_CRON_SUCCESS") {
    return
  }

  fmt.Println("SEND MAIL")

  user := os.Getenv("MAIL_SMTP_USERNAME")
  pass := os.Getenv("MAIL_SMTP_PASSWORD")
  sender := os.Getenv("MAIL_SENDER")
  to := make([]string, 0)

  s1 := strings.Split(recipients, ",")
  for _, s := range s1 {
    if s != "" {
      to = append(to, strings.Trim(s, " "))
    }
  }

  sub := ""
  if ok {
    sub = "Build successful"
  } else {
    sub = "ERROR: Error building website"
  }
  msg := []byte("From: " + sender + "\r\nTo: " + strings.Join(to, ", ") + "\r\n" + "Subject: " + sub + "\r\n" + "\r\n" + sb.String() + "\r\n")

  a := smtp.PlainAuth("", user, pass, srv)
  addr := fmt.Sprintf("%s:%s", srv, os.Getenv("MAIL_SMTP_PORT"))

  err := smtp.SendMail(addr, a, sender, to, msg)
  if err != nil {
    fmt.Println("error sending mail", err)
  }

}

func getBoolEnv(key string) bool {
  v := os.Getenv(key)
  if v == "" {
    return false
  }
  if b, err := strconv.ParseBool(os.Getenv(key)); err != nil {
    fmt.Println(err)
    return false
  } else {

    return b
  }
}

func checkout(branch string) error {

  cmd := exec.Command("git", "status", "--porcelain=v1")
  cmd.Dir = checkoutDir
  out, err := cmd.CombinedOutput()
  if err != nil {
    if strings.HasPrefix(string(out), "fatal: not a git repository") {
      fmt.Printf("cloning repo: %s\n", repoUrl)
      err = exec.Command("git", "clone", repoUrl, checkoutDir).Run()
      if err != nil {
        return err
      }
    } else {
      fmt.Print("status seems to be dirty, please check your git folder\n")
      fmt.Printf("git status: %s\n", err.Error())
      return err
    }
  }
  cmd = exec.Command("git", "checkout", branch)
  cmd.Dir = checkoutDir
  err = cmd.Run()
  if err != nil {
    fmt.Println(err)
  }

  cmd = exec.Command("git", "pull")
  cmd.Dir = checkoutDir
  err = cmd.Run()
  return err
}

func hugo(args ...string) (string, error) {

  cmd := exec.Command("/app/hugo", args...)
  cmd.Dir = checkoutDir
  out, err := cmd.CombinedOutput()
  if err != nil {
    return "", fmt.Errorf("%s\n%v\n", string(out), err)
  }
  return string(out), nil
}
