package main

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"

	log "github.com/Sirupsen/logrus"
	"github.com/codegangsta/cli"
	"github.com/skarademir/naturalsort"

	"github.com/docker/machine/drivers"
	_ "github.com/docker/machine/drivers/amazonec2"
	_ "github.com/docker/machine/drivers/azure"
	_ "github.com/docker/machine/drivers/digitalocean"
	_ "github.com/docker/machine/drivers/google"
	_ "github.com/docker/machine/drivers/hyperv"
	_ "github.com/docker/machine/drivers/none"
	_ "github.com/docker/machine/drivers/openstack"
	_ "github.com/docker/machine/drivers/rackspace"
	_ "github.com/docker/machine/drivers/softlayer"
	_ "github.com/docker/machine/drivers/virtualbox"
	_ "github.com/docker/machine/drivers/vmwarefusion"
	_ "github.com/docker/machine/drivers/vmwarevcloudair"
	_ "github.com/docker/machine/drivers/vmwarevsphere"
	"github.com/docker/machine/state"
	"github.com/docker/machine/utils"
)

type machineConfig struct {
	machineName    string
	machineDir     string
	caCertPath     string
	caKeyPath      string
	clientCertPath string
	clientKeyPath  string
	serverCertPath string
	serverKeyPath  string
	machineUrl     string
	swarmMaster    bool
	swarmHost      string
	swarmDiscovery string
}

type hostListItem struct {
	Name           string
	Active         bool
	DriverName     string
	State          state.State
	URL            string
	SwarmMaster    bool
	SwarmDiscovery string
}

func sortHostListItemsByName(items []hostListItem) {
	m := make(map[string]hostListItem, len(items))
	s := make([]string, len(items))
	for i, v := range items {
		name := strings.ToLower(v.Name)
		m[name] = v
		s[i] = name
	}
	sort.Sort(naturalsort.NaturalSort(s))
	for i, v := range s {
		items[i] = m[v]
	}
}

func confirmInput(msg string) bool {
	fmt.Printf("%s (y/n): ", msg)
	var resp string
	_, err := fmt.Scanln(&resp)

	if err != nil {
		log.Fatal(err)

	}

	if strings.Index(strings.ToLower(resp), "y") == 0 {
		return true

	}

	return false

}

func setupCertificates(caCertPath, caKeyPath, clientCertPath, clientKeyPath string) error {
	org := utils.GetUsername()
	bits := 2048

	if _, err := os.Stat(utils.GetMachineCertDir()); err != nil {
		if os.IsNotExist(err) {
			if err := os.MkdirAll(utils.GetMachineCertDir(), 0700); err != nil {
				log.Fatalf("Error creating machine config dir: %s", err)
			}
		} else {
			log.Fatal(err)
		}
	}

	if _, err := os.Stat(caCertPath); os.IsNotExist(err) {
		log.Infof("Creating CA: %s", caCertPath)

		// check if the key path exists; if so, error
		if _, err := os.Stat(caKeyPath); err == nil {
			log.Fatalf("The CA key already exists.  Please remove it or specify a different key/cert.")
		}

		if err := utils.GenerateCACertificate(caCertPath, caKeyPath, org, bits); err != nil {
			log.Infof("Error generating CA certificate: %s", err)
		}
	}

	if _, err := os.Stat(clientCertPath); os.IsNotExist(err) {
		log.Infof("Creating client certificate: %s", clientCertPath)

		if _, err := os.Stat(utils.GetMachineCertDir()); err != nil {
			if os.IsNotExist(err) {
				if err := os.Mkdir(utils.GetMachineCertDir(), 0700); err != nil {
					log.Fatalf("Error creating machine client cert dir: %s", err)
				}
			} else {
				log.Fatal(err)
			}
		}

		// check if the key path exists; if so, error
		if _, err := os.Stat(clientKeyPath); err == nil {
			log.Fatalf("The client key already exists.  Please remove it or specify a different key/cert.")
		}

		if err := utils.GenerateCert([]string{""}, clientCertPath, clientKeyPath, caCertPath, caKeyPath, org, bits); err != nil {
			log.Fatalf("Error generating client certificate: %s", err)
		}
	}

	return nil
}

var Commands = []cli.Command{
	{
		Name:   "active",
		Usage:  "Get or set the active machine",
		Action: cmdActive,
	},
	{
		Flags: append(
			drivers.GetCreateFlags(),
			cli.StringFlag{
				Name: "driver, d",
				Usage: fmt.Sprintf(
					"Driver to create machine with. Available drivers: %s",
					strings.Join(drivers.GetDriverNames(), ", "),
				),
				Value: "none",
			},
			cli.BoolFlag{
				Name:  "swarm",
				Usage: "Configure Machine with Swarm",
			},
			cli.BoolFlag{
				Name:  "swarm-master",
				Usage: "Configure Machine to be a Swarm master",
			},
			cli.StringFlag{
				Name:  "swarm-discovery",
				Usage: "Discovery service to use with Swarm",
				Value: "",
			},
			cli.StringFlag{
				Name:  "swarm-host",
				Usage: "ip/socket to listen on for Swarm master",
				Value: "tcp://0.0.0.0:3376",
			},
			cli.StringFlag{
				Name:  "swarm-addr",
				Usage: "addr to advertise for Swarm (default: detect and use the machine IP)",
				Value: "",
			},
		),
		Name:   "create",
		Usage:  "Create a machine",
		Action: cmdCreate,
	},
	{
		Name:        "config",
		Usage:       "Print the connection config for machine",
		Description: "Argument is a machine name. Will use the active machine if none is provided.",
		Action:      cmdConfig,
		Flags: []cli.Flag{
			cli.BoolFlag{
				Name:  "swarm",
				Usage: "Display the Swarm config instead of the Docker daemon",
			},
		},
	},
	{
		Name:        "inspect",
		Usage:       "Inspect information about a machine",
		Description: "Argument is a machine name. Will use the active machine if none is provided.",
		Action:      cmdInspect,
	},
	{
		Name:        "ip",
		Usage:       "Get the IP address of a machine",
		Description: "Argument is a machine name. Will use the active machine if none is provided.",
		Action:      cmdIp,
	},
	{
		Name:        "kill",
		Usage:       "Kill a machine",
		Description: "Argument(s) are one or more machine names. Will use the active machine if none is provided.",
		Action:      cmdKill,
	},
	{
		Flags: []cli.Flag{
			cli.BoolFlag{
				Name:  "quiet, q",
				Usage: "Enable quiet mode",
			},
		},
		Name:   "ls",
		Usage:  "List machines",
		Action: cmdLs,
	},
	{
		Name:        "regenerate-certs",
		Usage:       "Regenerate TLS Certificates for a machine",
		Description: "Argument(s) are one or more machine names.  Will use the active machine if none is provided.",
		Action:      cmdRegenerateCerts,
		Flags: []cli.Flag{
			cli.BoolFlag{
				Name:  "force, f",
				Usage: "Force rebuild and do not prompt",
			},
		},
	},
	{
		Name:        "restart",
		Usage:       "Restart a machine",
		Description: "Argument(s) are one or more machine names. Will use the active machine if none is provided.",
		Action:      cmdRestart,
	},
	{
		Flags: []cli.Flag{
			cli.BoolFlag{
				Name:  "force, f",
				Usage: "Remove local configuration even if machine cannot be removed",
			},
		},
		Name:        "rm",
		Usage:       "Remove a machine",
		Description: "Argument(s) are one or more machine names.",
		Action:      cmdRm,
	},
	{
		Name:        "env",
		Usage:       "Display the commands to set up the environment for the Docker client",
		Description: "Argument is a machine name. Will use the active machine if none is provided.",
		Action:      cmdEnv,
		Flags: []cli.Flag{
			cli.BoolFlag{
				Name:  "swarm",
				Usage: "Display the Swarm config instead of the Docker daemon",
			},
			cli.BoolFlag{
				Name:  "unset, u",
				Usage: "Unset variables instead of setting them",
			},
		},
	},
	{
		Name:        "ssh",
		Usage:       "Log into or run a command on a machine with SSH",
		Description: "Arguments are [machine-name] command - Will use the active machine if none is provided.",
		Action:      cmdSsh,
	},
	{
		Name:        "start",
		Usage:       "Start a machine",
		Description: "Argument(s) are one or more machine names. Will use the active machine if none is provided.",
		Action:      cmdStart,
	},
	{
		Name:        "stop",
		Usage:       "Stop a machine",
		Description: "Argument(s) are one or more machine names. Will use the active machine if none is provided.",
		Action:      cmdStop,
	},
	{
		Name:        "upgrade",
		Usage:       "Upgrade a machine to the latest version of Docker",
		Description: "Argument(s) are one or more machine names. Will use the active machine if none is provided.",
		Action:      cmdUpgrade,
	},
	{
		Name:        "url",
		Usage:       "Get the URL of a machine",
		Description: "Argument is a machine name. Will use the active machine if none is provided.",
		Action:      cmdUrl,
	},
}

func cmdActive(c *cli.Context) {
	name := c.Args().First()
	store := NewStore(utils.GetMachineDir(), c.GlobalString("tls-ca-cert"), c.GlobalString("tls-ca-key"))

	if name == "" {
		host, err := store.GetActive()
		if err != nil {
			log.Fatalf("error getting active host: %v", err)
		}
		if host != nil {
			fmt.Println(host.Name)
		}
	} else if name != "" {
		host, err := store.Load(name)
		if err != nil {
			log.Fatalf("error loading host: %v", err)
		}

		if err := store.SetActive(host); err != nil {
			log.Fatalf("error setting active host: %v", err)
		}
	} else {
		cli.ShowCommandHelp(c, "active")
	}
}

func cmdCreate(c *cli.Context) {
	driver := c.String("driver")
	name := c.Args().First()

	if name == "" {
		cli.ShowCommandHelp(c, "create")
		log.Fatal("You must specify a machine name")
	}

	if err := setupCertificates(c.GlobalString("tls-ca-cert"), c.GlobalString("tls-ca-key"),
		c.GlobalString("tls-client-cert"), c.GlobalString("tls-client-key")); err != nil {
		log.Fatalf("Error generating certificates: %s", err)
	}

	store := NewStore(utils.GetMachineDir(), c.GlobalString("tls-ca-cert"), c.GlobalString("tls-ca-key"))

	host, err := store.Create(name, driver, c)
	if err != nil {
		log.Errorf("Error creating machine: %s", err)
		log.Warn("You will want to check the provider to make sure the machine and associated resources were properly removed.")
		log.Fatal("Error creating machine")
	}
	if err := store.SetActive(host); err != nil {
		log.Fatalf("error setting active host: %v", err)
	}

	info := ""
	userShell := filepath.Base(os.Getenv("SHELL"))

	switch userShell {
	case "fish":
		info = fmt.Sprintf("%s env %s | source", c.App.Name, name)
	default:
		info = fmt.Sprintf(`eval "$(%s env %s)"`, c.App.Name, name)
	}

	log.Infof("%q has been created and is now the active machine.", name)

	if info != "" {
		log.Infof("To point your Docker client at it, run this in your shell: %s", info)
	}
}

func cmdConfig(c *cli.Context) {
	cfg, err := getMachineConfig(c)
	if err != nil {
		log.Fatal(err)
	}

	dockerHost, err := getHost(c).Driver.GetURL()
	if err != nil {
		log.Fatal(err)
	}

	if c.Bool("swarm") {
		if !cfg.swarmMaster {
			log.Fatalf("%s is not a swarm master", cfg.machineName)
		}
		u, err := url.Parse(cfg.swarmHost)
		if err != nil {
			log.Fatal(err)
		}
		parts := strings.Split(u.Host, ":")
		swarmPort := parts[1]

		// get IP of machine to replace in case swarm host is 0.0.0.0
		mUrl, err := url.Parse(dockerHost)
		if err != nil {
			log.Fatal(err)
		}
		mParts := strings.Split(mUrl.Host, ":")
		machineIp := mParts[0]

		dockerHost = fmt.Sprintf("tcp://%s:%s", machineIp, swarmPort)
	}

	log.Debug(dockerHost)

	u, err := url.Parse(cfg.machineUrl)
	if err != nil {
		log.Fatal(err)
	}

	if u.Scheme != "unix" {
		// validate cert and regenerate if needed
		valid, err := utils.ValidateCertificate(
			u.Host,
			cfg.caCertPath,
			cfg.serverCertPath,
			cfg.serverKeyPath,
		)
		if err != nil {
			log.Fatal(err)
		}

		if !valid {
			log.Debugf("invalid certs detected; regenerating for %s", u.Host)

			if err := runActionWithContext("configureAuth", c); err != nil {
				log.Fatal(err)
			}
		}
	}

	fmt.Printf("--tlsverify --tlscacert=%q --tlscert=%q --tlskey=%q -H=%s",
		cfg.caCertPath, cfg.clientCertPath, cfg.clientKeyPath, dockerHost)
}

func cmdInspect(c *cli.Context) {
	prettyJSON, err := json.MarshalIndent(getHost(c), "", "    ")
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println(string(prettyJSON))
}

func cmdIp(c *cli.Context) {
	ip, err := getHost(c).Driver.GetIP()
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println(ip)
}

func cmdLs(c *cli.Context) {
	quiet := c.Bool("quiet")
	store := NewStore(utils.GetMachineDir(), c.GlobalString("tls-ca-cert"), c.GlobalString("tls-ca-key"))

	hostList, err := store.List()
	if err != nil {
		log.Fatal(err)
	}

	w := tabwriter.NewWriter(os.Stdout, 5, 1, 3, ' ', 0)

	if !quiet {
		fmt.Fprintln(w, "NAME\tACTIVE\tDRIVER\tSTATE\tURL\tSWARM")
	}

	items := []hostListItem{}
	hostListItems := make(chan hostListItem)

	swarmMasters := make(map[string]string)
	swarmInfo := make(map[string]string)

	for _, host := range hostList {
		if !quiet {
			if host.SwarmMaster {
				swarmMasters[host.SwarmDiscovery] = host.Name
			}

			if host.SwarmDiscovery != "" {
				swarmInfo[host.Name] = host.SwarmDiscovery
			}

			go getHostState(host, *store, hostListItems)
		} else {
			fmt.Fprintf(w, "%s\n", host.Name)
		}
	}

	if !quiet {
		for i := 0; i < len(hostList); i++ {
			items = append(items, <-hostListItems)
		}
	}

	close(hostListItems)

	sortHostListItemsByName(items)

	for _, item := range items {
		activeString := ""
		if item.Active {
			activeString = "*"
		}

		swarmInfo := ""

		if item.SwarmDiscovery != "" {
			swarmInfo = swarmMasters[item.SwarmDiscovery]
			if item.SwarmMaster {
				swarmInfo = fmt.Sprintf("%s (master)", swarmInfo)
			}
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n",
			item.Name, activeString, item.DriverName, item.State, item.URL, swarmInfo)
	}

	w.Flush()
}

func cmdRegenerateCerts(c *cli.Context) {
	force := c.Bool("force")
	if force || confirmInput("Regenerate TLS machine certs?  Warning: this is irreversible.") {
		log.Infof("Regenerating TLS certificates")
		if err := runActionWithContext("configureAuth", c); err != nil {
			log.Fatal(err)
		}
	}
}

func cmdRm(c *cli.Context) {
	if len(c.Args()) == 0 {
		cli.ShowCommandHelp(c, "rm")
		log.Fatal("You must specify a machine name")
	}

	force := c.Bool("force")

	isError := false

	store := NewStore(utils.GetMachineDir(), c.GlobalString("tls-ca-cert"), c.GlobalString("tls-ca-key"))
	for _, host := range c.Args() {
		if err := store.Remove(host, force); err != nil {
			log.Errorf("Error removing machine %s: %s", host, err)
			isError = true
		}
	}
	if isError {
		log.Fatal("There was an error removing a machine. To force remove it, pass the -f option. Warning: this might leave it running on the provider.")
	}
}

func cmdEnv(c *cli.Context) {
	userShell := filepath.Base(os.Getenv("SHELL"))
	if c.Bool("unset") {
		switch userShell {
		case "fish":
			fmt.Printf("set -e DOCKER_TLS_VERIFY;\nset -e DOCKER_CERT_PATH;\nset -e DOCKER_HOST;\n")
		default:
			fmt.Println("unset DOCKER_TLS_VERIFY DOCKER_CERT_PATH DOCKER_HOST")
		}
		return
	}

	cfg, err := getMachineConfig(c)
	if err != nil {
		log.Fatal(err)
	}

	dockerHost := cfg.machineUrl
	if c.Bool("swarm") {
		if !cfg.swarmMaster {
			log.Fatalf("%s is not a swarm master", cfg.machineName)
		}
		u, err := url.Parse(cfg.swarmHost)
		if err != nil {
			log.Fatal(err)
		}
		parts := strings.Split(u.Host, ":")
		swarmPort := parts[1]

		// get IP of machine to replace in case swarm host is 0.0.0.0
		mUrl, err := url.Parse(cfg.machineUrl)
		if err != nil {
			log.Fatal(err)
		}
		mParts := strings.Split(mUrl.Host, ":")
		machineIp := mParts[0]

		dockerHost = fmt.Sprintf("tcp://%s:%s", machineIp, swarmPort)
	}

	u, err := url.Parse(cfg.machineUrl)
	if err != nil {
		log.Fatal(err)
	}

	if u.Scheme != "unix" {
		// validate cert and regenerate if needed
		valid, err := utils.ValidateCertificate(
			u.Host,
			cfg.caCertPath,
			cfg.serverCertPath,
			cfg.serverKeyPath,
		)
		if err != nil {
			log.Fatal(err)
		}

		if !valid {
			log.Debugf("invalid certs detected; regenerating for %s", u.Host)

			if err := runActionWithContext("configureAuth", c); err != nil {
				log.Fatal(err)
			}
		}
	}

	switch userShell {
	case "fish":
		fmt.Printf("set -x DOCKER_TLS_VERIFY 1;\nset -x DOCKER_CERT_PATH %q;\nset -x DOCKER_HOST %s;\n",
			cfg.machineDir, dockerHost)
	default:
		fmt.Printf("export DOCKER_TLS_VERIFY=1\nexport DOCKER_CERT_PATH=%q\nexport DOCKER_HOST=%s\n",
			cfg.machineDir, dockerHost)
	}
}

func cmdSsh(c *cli.Context) {
	var (
		err    error
		sshCmd *exec.Cmd
	)
	name := c.Args().First()
	store := NewStore(utils.GetMachineDir(), c.GlobalString("tls-ca-cert"), c.GlobalString("tls-ca-key"))

	if name == "" {
		host, err := store.GetActive()
		if err != nil {
			log.Fatalf("unable to get active host: %v", err)
		}

		name = host.Name
	}

	host, err := store.Load(name)
	if err != nil {
		log.Fatal(err)
	}

	if len(c.Args()) <= 1 {
		sshCmd, err = host.GetSSHCommand()
	} else {
		sshCmd, err = host.GetSSHCommand(c.Args()[1:]...)
	}
	if err != nil {
		log.Fatal(err)
	}

	sshCmd.Stdin = os.Stdin
	sshCmd.Stdout = os.Stdout
	sshCmd.Stderr = os.Stderr
	if err := sshCmd.Run(); err != nil {
		log.Fatal(err)
	}
}

// machineCommand maps the command name to the corresponding machine command.
// We run commands concurrently and communicate back an error if there was one.
func machineCommand(actionName string, machine *Host, errorChan chan<- error) {
	commands := map[string](func() error){
		"configureAuth": machine.ConfigureAuth,
		"start":         machine.Start,
		"stop":          machine.Stop,
		"restart":       machine.Restart,
		"kill":          machine.Kill,
		"upgrade":       machine.Upgrade,
	}

	log.Debugf("command=%s machine=%s", actionName, machine.Name)

	if err := commands[actionName](); err != nil {
		errorChan <- err
		return
	}

	errorChan <- nil
}

// runActionForeachMachine will run the command across multiple machines
func runActionForeachMachine(actionName string, machines []*Host) {
	var (
		numConcurrentActions = 0
		serialMachines       = []*Host{}
		errorChan            = make(chan error)
	)

	for _, machine := range machines {
		// Virtualbox is temperamental about doing things concurrently,
		// so we schedule the actions in a "queue" to be executed serially
		// after the concurrent actions are scheduled.
		switch machine.DriverName {
		case "virtualbox":
			machine := machine
			serialMachines = append(serialMachines, machine)
		default:
			numConcurrentActions++
			go machineCommand(actionName, machine, errorChan)
		}
	}

	// While the concurrent actions are running,
	// do the serial actions.  As the name implies,
	// these run one at a time.
	for _, machine := range serialMachines {
		serialChan := make(chan error)
		go machineCommand(actionName, machine, serialChan)
		if err := <-serialChan; err != nil {
			log.Errorln(err)
		}
		close(serialChan)
	}

	// TODO: We should probably only do 5-10 of these
	// at a time, since otherwise cloud providers might
	// rate limit us.
	for i := 0; i < numConcurrentActions; i++ {
		if err := <-errorChan; err != nil {
			log.Errorln(err)
		}
	}

	close(errorChan)
}

func runActionWithContext(actionName string, c *cli.Context) error {
	machines, err := getHosts(c)
	if err != nil {
		return err
	}

	// No args specified, so use active.
	if len(machines) == 0 {
		store := NewStore(utils.GetMachineDir(), c.GlobalString("tls-ca-cert"), c.GlobalString("tls-ca-key"))
		activeHost, err := store.GetActive()
		if err != nil {
			log.Fatalf("Unable to get active host: %v", err)
		}
		machines = []*Host{activeHost}
	}

	runActionForeachMachine(actionName, machines)

	return nil
}

func cmdStart(c *cli.Context) {
	if err := runActionWithContext("start", c); err != nil {
		log.Fatal(err)
	}
}

func cmdStop(c *cli.Context) {
	if err := runActionWithContext("stop", c); err != nil {
		log.Fatal(err)
	}
}

func cmdRestart(c *cli.Context) {
	if err := runActionWithContext("restart", c); err != nil {
		log.Fatal(err)
	}
}

func cmdKill(c *cli.Context) {
	if err := runActionWithContext("kill", c); err != nil {
		log.Fatal(err)
	}
}

func cmdUpgrade(c *cli.Context) {
	if err := runActionWithContext("upgrade", c); err != nil {
		log.Fatal(err)
	}
}

func cmdUrl(c *cli.Context) {
	url, err := getHost(c).GetURL()
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println(url)
}

func cmdNotFound(c *cli.Context, command string) {
	log.Fatalf(
		"%s: '%s' is not a %s command. See '%s --help'.",
		c.App.Name,
		command,
		c.App.Name,
		c.App.Name,
	)
}

func getHosts(c *cli.Context) ([]*Host, error) {
	machines := []*Host{}
	for _, n := range c.Args() {
		machine, err := loadMachine(n, c)
		if err != nil {
			return nil, err
		}

		machines = append(machines, machine)
	}

	return machines, nil
}

func loadMachine(name string, c *cli.Context) (*Host, error) {
	store := NewStore(utils.GetMachineDir(), c.GlobalString("tls-ca-cert"), c.GlobalString("tls-ca-key"))

	machine, err := store.Load(name)
	if err != nil {
		return nil, err
	}

	return machine, nil
}

func getHost(c *cli.Context) *Host {
	name := c.Args().First()
	store := NewStore(utils.GetMachineDir(), c.GlobalString("tls-ca-cert"), c.GlobalString("tls-ca-key"))

	if name == "" {
		host, err := store.GetActive()
		if err != nil {
			log.Fatalf("unable to get active host: %v", err)
		}

		if host == nil {
			log.Fatal("unable to get active host, active file not found")
		}
		return host
	}

	host, err := store.Load(name)
	if err != nil {
		log.Fatalf("unable to load host: %v", err)
	}
	return host
}

func getHostState(host Host, store Store, hostListItems chan<- hostListItem) {
	currentState, err := host.Driver.GetState()
	if err != nil {
		log.Errorf("error getting state for host %s: %s", host.Name, err)
	}

	url, err := host.GetURL()
	if err != nil {
		if err == drivers.ErrHostIsNotRunning {
			url = ""
		} else {
			log.Errorf("error getting URL for host %s: %s", host.Name, err)
		}
	}

	isActive, err := store.IsActive(&host)
	if err != nil {
		log.Debugf("error determining whether host %q is active: %s",
			host.Name, err)
	}

	hostListItems <- hostListItem{
		Name:           host.Name,
		Active:         isActive,
		DriverName:     host.Driver.DriverName(),
		State:          currentState,
		URL:            url,
		SwarmMaster:    host.SwarmMaster,
		SwarmDiscovery: host.SwarmDiscovery,
	}
}

func getMachineConfig(c *cli.Context) (*machineConfig, error) {
	name := c.Args().First()
	store := NewStore(utils.GetMachineDir(), c.GlobalString("tls-ca-cert"), c.GlobalString("tls-ca-key"))
	var machine *Host

	if name == "" {
		m, err := store.GetActive()
		if err != nil {
			log.Fatalf("error getting active host: %v", err)
		}
		if m == nil {
			return nil, fmt.Errorf("There is no active host")
		}
		machine = m
	} else {
		m, err := store.Load(name)
		if err != nil {
			return nil, fmt.Errorf("Error loading machine config: %s", err)
		}
		machine = m
	}

	machineDir := filepath.Join(utils.GetMachineDir(), machine.Name)
	caCert := filepath.Join(machineDir, "ca.pem")
	caKey := filepath.Join(utils.GetMachineCertDir(), "ca-key.pem")
	clientCert := filepath.Join(machineDir, "cert.pem")
	clientKey := filepath.Join(machineDir, "key.pem")
	serverCert := filepath.Join(machineDir, "server.pem")
	serverKey := filepath.Join(machineDir, "server-key.pem")
	machineUrl, err := machine.GetURL()
	if err != nil {
		if err == drivers.ErrHostIsNotRunning {
			machineUrl = ""
		} else {
			return nil, fmt.Errorf("Unexpected error getting machine url: %s", err)
		}
	}
	return &machineConfig{
		machineName:    name,
		machineDir:     machineDir,
		caCertPath:     caCert,
		caKeyPath:      caKey,
		clientCertPath: clientCert,
		clientKeyPath:  clientKey,
		serverCertPath: serverCert,
		serverKeyPath:  serverKey,
		machineUrl:     machineUrl,
		swarmMaster:    machine.SwarmMaster,
		swarmHost:      machine.SwarmHost,
		swarmDiscovery: machine.SwarmDiscovery,
	}, nil
}
