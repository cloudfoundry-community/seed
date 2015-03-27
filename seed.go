package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/cloudfoundry-community/cftype"
	"github.com/cloudfoundry/cli/cf/api/resources"
	"github.com/cloudfoundry/cli/cf/configuration/config_helpers"
	"github.com/cloudfoundry/cli/cf/configuration/core_config"
	"github.com/cloudfoundry/cli/plugin"
	"github.com/codegangsta/cli"
	"gopkg.in/yaml.v2"
)

func fatalIf(err error) {
	if err != nil {
		fmt.Fprintln(os.Stdout, "error:", err)
		os.Exit(1)
	}
}

func main() {
	plugin.Start(&SeedPlugin{})
}

//SeedPlugin empty struct for plugin
type SeedPlugin struct{}

//Run of seeder plugin
func (plugin SeedPlugin) Run(cliConnection plugin.CliConnection, args []string) {
	app := cli.NewApp()
	app.Name = "seed"
	app.Version = VERSION
	app.Author = "Long Nguyen"
	app.Email = "long.nguyen11288@gmail.com"
	app.Usage = "Seeds Cloud Foundry and setups apps/orgs/services on a given Cloud Foundry setup"
	app.Flags = []cli.Flag{
		cli.StringFlag{
			Name:  "f",
			Value: "",
			Usage: "seed manifest for seeding Cloud Foundry",
		},
		cli.BoolFlag{
			Name:  "c",
			Usage: "cleanup all things created by the manifest",
		},
	}
	app.Action = func(c *cli.Context) {
		if !c.IsSet("f") {
			cli.ShowAppHelp(c)
			os.Exit(1)
		}
		fileName := c.String("f")
		seedRepo := NewSeedRepo(cliConnection, fileName)

		err := seedRepo.readManifest()
		fatalIf(err)

		if c.Bool("c") {
			err = seedRepo.deleteApps()
			fatalIf(err)

			err = seedRepo.deleteServices()
			fatalIf(err)

			err = seedRepo.deleteSpaces()
			fatalIf(err)

			err = seedRepo.deleteOrganizations()
			fatalIf(err)
		} else {
			err = seedRepo.createOrganizations()
			fatalIf(err)

			err = seedRepo.createSpaces()
			fatalIf(err)

			err = seedRepo.createApps()
			fatalIf(err)

			err = seedRepo.createServices()
			fatalIf(err)
		}
	}
	app.Run(args)
}

//GetMetadata of plugin
func (SeedPlugin) GetMetadata() plugin.PluginMetadata {
	versionParts := strings.Split(string(VERSION), ".")
	major, _ := strconv.Atoi(versionParts[0])
	minor, _ := strconv.Atoi(versionParts[1])
	patch, _ := strconv.Atoi(strings.TrimSpace(versionParts[2]))

	return plugin.PluginMetadata{
		Name: "cf-plugin-seed",
		Version: plugin.VersionType{
			Major: major,
			Minor: minor,
			Build: patch,
		},
		Commands: []plugin.Command{
			{
				Name:     "seed",
				HelpText: "Seeds Cloud Foundry and setups apps/orgs/services on new Cloud Foundry setup",
			},
		},
	}
}

//SeedRepo of cli
type SeedRepo struct {
	conn     plugin.CliConnection
	fileName string
	Manifest SeederManifest
}

func NewSeedRepo(conn plugin.CliConnection, fileName string) *SeedRepo {
	return &SeedRepo{
		conn:     conn,
		fileName: fileName,
	}
}

func (repo *SeedRepo) readManifest() error {
	file, err := ioutil.ReadFile(repo.fileName)
	if err != nil {
		return err
	}
	repo.Manifest = SeederManifest{}

	err = yaml.Unmarshal(file, &repo.Manifest)
	if err != nil {
		return err
	}

	return nil
}

func (repo *SeedRepo) createOrganizations() error {
	for _, org := range repo.Manifest.Organizations {
		_, err := repo.conn.CliCommand("create-org", org.Name)
		if err != nil {
			return err
		}
	}
	return nil
}

func (repo *SeedRepo) deleteOrganizations() error {
	for _, org := range repo.Manifest.Organizations {
		_, err := repo.conn.CliCommand("delete-org", org.Name, "-f")
		if err != nil {
			return err
		}
	}
	return nil
}

func (repo *SeedRepo) createSpaces() error {
	for _, org := range repo.Manifest.Organizations {
		repo.conn.CliCommand("target", "-o", org.Name)
		for _, space := range org.Spaces {
			_, err := repo.conn.CliCommand("create-space", space.Name)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func (repo *SeedRepo) deleteSpaces() error {
	for _, org := range repo.Manifest.Organizations {
		repo.conn.CliCommand("target", "-o", org.Name)
		for _, space := range org.Spaces {
			_, err := repo.conn.CliCommand("delete-space", space.Name, "-f")
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func (repo *SeedRepo) createServices() error {
	for _, org := range repo.Manifest.Organizations {
		for _, space := range org.Spaces {
			repo.conn.CliCommand("target", "-o", org.Name, "-s", space.Name)
			for _, service := range space.Services {
				_, err := repo.conn.CliCommand("create-service", service.Service, service.Plan, service.Name)
				if err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func (repo *SeedRepo) deleteServices() error {
	for _, org := range repo.Manifest.Organizations {
		for _, space := range org.Spaces {
			repo.conn.CliCommand("target", "-o", org.Name, "-s", space.Name)
			for _, service := range space.Services {
				_, err := repo.conn.CliCommand("delete-service", service.Name, "-f")
				if err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func (repo *SeedRepo) createApps() error {
	for _, org := range repo.Manifest.Organizations {
		for _, space := range org.Spaces {
			repo.conn.CliCommand("target", "-o", org.Name, "-s", space.Name)
			for _, app := range space.Apps {
				err := repo.deployApp(app)
				if err != nil {
					return err
				}
				emptyServiceBroker := ServiceBroker{}
				if app.ServiceBroker != emptyServiceBroker {
					fmt.Println("setting app as service")
					err := repo.setAppAsService(app)
					if err != nil {
						return err
					}
				}
			}
		}
	}
	return nil
}

func (repo *SeedRepo) deleteApps() error {
	for _, org := range repo.Manifest.Organizations {
		for _, space := range org.Spaces {
			repo.conn.CliCommand("target", "-o", org.Name, "-s", space.Name)
			for _, app := range space.Apps {
				emptyServiceBroker := ServiceBroker{}
				if app.ServiceBroker != emptyServiceBroker {
					err := repo.deleteAppAsService(app)
					if err != nil {
						return err
					}
				}
				err := repo.deleteApp(app)
				if err != nil {
					return err
				}
			}
		}
	}
	return nil
}

//DeleteApp deletes a single app
func (repo *SeedRepo) deleteApp(app deployApp) error {

	_, err := repo.conn.CliCommand("delete", app.Name, "-f", "-r")
	if err != nil {
		return err
	}

	return nil
}

//deployApp deploys a single app
func (repo *SeedRepo) deployApp(app deployApp) error {
	args := []string{"push", app.Name}
	if app.Repo != "" {
		wd, _ := os.Getwd()
		appPath := wd + "/apps/" + app.Name
		os.MkdirAll(appPath, 0777)

		files, _ := ioutil.ReadDir(appPath)

		if len(files) == 0 {
			gitPath, err := exec.LookPath("git")
			if err != nil {
				return err
			}
			err = exec.Command(gitPath, "clone", app.Repo, appPath).Run()
			if err != nil {
				return nil
			}
		}
		args = append(args, "-p", appPath)

	} else if app.Path != "" {
		args = append(args, "-p", app.Path)
	} else {
		errMsg := fmt.Sprintf("App need repo or path %s", app.Name)
		return errors.New(errMsg)
	}

	if app.Disk != "" {
		args = append(args, "-k", app.Disk)
	}
	if app.Memory != "" {
		args = append(args, "-m", app.Memory)
	}
	if app.Instances != "" {
		args = append(args, "-i", app.Instances)
	}
	if app.Hostname != "" {
		args = append(args, "-n", app.Hostname)
	}
	if app.Domain != "" {
		args = append(args, "-d", app.Domain)
	}
	if app.Buildpack != "" {
		args = append(args, "-b", app.Buildpack)
	}
	if app.Manifest != "" {
		args = append(args, "-f", app.Manifest)
	}

	repo.conn.CliCommand(args...)

	return nil
}

func (repo *SeedRepo) setAppAsService(app deployApp) error {
	appInfo := repo.getAppInfo(app)
	appRoute, err := repo.firstAppRoute(appInfo)
	if err != nil {
		return err
	}
	app.ServiceBroker.Url = "https://" + appRoute
	err = repo.createServiceBroker(app.ServiceBroker)
	if err != nil {
		return err
	}
	for _, service := range app.ServiceAccess {
		err := repo.enableServiceAccess(service)
		if err != nil {
			return err
		}
	}
	return nil
}

func (repo *SeedRepo) deleteAppAsService(app deployApp) error {
	appInfo := repo.getAppInfo(app)
	appRoute, err := repo.firstAppRoute(appInfo)
	if err != nil {
		return err
	}
	app.ServiceBroker.Url = "https://" + appRoute
	for _, service := range app.ServiceAccess {
		err := repo.disableServiceAccess(service)
		if err != nil {
			return err
		}
	}
	err = repo.deleteServiceBroker(app.ServiceBroker)
	if err != nil {
		return err
	}
	return nil
}

func (repo *SeedRepo) getAppInfo(app deployApp) *cftype.RetrieveAParticularApp {
	confRepo := core_config.NewRepositoryFromFilepath(config_helpers.DefaultFilePath(), fatalIf)
	spaceGUID := confRepo.SpaceFields().Guid

	appGUID := repo.findAppGUID(spaceGUID, app.Name)

	appInfo := repo.findApp(appGUID)
	return appInfo
}

func (repo *SeedRepo) firstAppRoute(app *cftype.RetrieveAParticularApp) (fullRoute string, err error) {
	routes := &cftype.ListAllRoutesForTheApp{}
	cmd := []string{"curl", app.Entity.RoutesURL}
	output, _ := repo.conn.CliCommandWithoutTerminalOutput(cmd...)
	json.Unmarshal([]byte(strings.Join(output, "")), &routes)

	if routes.TotalResults == 0 {
		return "", fmt.Errorf("App '%s' has no routes", app.Entity.Name)
	}
	route := routes.Resources[0]

	domain := &cftype.RetrieveAParticularDomain{}
	cmd = []string{"curl", route.Entity.DomainURL}
	output, _ = repo.conn.CliCommandWithoutTerminalOutput(cmd...)
	json.Unmarshal([]byte(strings.Join(output, "")), &domain)

	if route.Entity.Host != "" {
		return fmt.Sprintf("%s.%s", route.Entity.Host, domain.Entity.Name), nil
	}
	return domain.Entity.Name, nil
}

func (repo *SeedRepo) findApp(appGUID string) (app *cftype.RetrieveAParticularApp) {
	app = &cftype.RetrieveAParticularApp{}
	cmd := []string{"curl", fmt.Sprintf("/v2/apps/%s", appGUID)}
	output, _ := repo.conn.CliCommandWithoutTerminalOutput(cmd...)
	json.Unmarshal([]byte(strings.Join(output, "")), &app)
	return app
}

func (repo *SeedRepo) findAppGUID(spaceGUID string, appName string) string {
	appQuery := fmt.Sprintf("/v2/spaces/%v/apps?q=name:%v&inline-relations-depth=1", spaceGUID, appName)
	cmd := []string{"curl", appQuery}

	output, _ := repo.conn.CliCommandWithoutTerminalOutput(cmd...)
	res := &resources.PaginatedApplicationResources{}
	json.Unmarshal([]byte(strings.Join(output, "")), &res)

	return res.Resources[0].Resource.Metadata.Guid
}

func (repo *SeedRepo) createServiceBroker(broker ServiceBroker) error {
	args := []string{"create-service-broker", broker.Name, broker.Username, broker.Password, broker.Url}
	_, err := repo.conn.CliCommand(args...)
	return err
}

func (repo *SeedRepo) deleteServiceBroker(broker ServiceBroker) error {
	args := []string{"delete-service-broker", broker.Name, "-f"}
	_, err := repo.conn.CliCommand(args...)
	return err
}

func (repo *SeedRepo) enableServiceAccess(service Service) error {
	args := []string{"enable-service-access", service.Service}
	if service.Plan != "" {
		args = append(args, "-p", service.Plan)
	}
	if service.Org != "" {
		args = append(args, "-o", service.Org)
	}
	_, err := repo.conn.CliCommand(args...)
	return err
}

func (repo *SeedRepo) disableServiceAccess(service Service) error {
	args := []string{"disable-service-access", service.Service}
	if service.Plan != "" {
		args = append(args, "-p", service.Plan)
	}
	if service.Org != "" {
		args = append(args, "-o", service.Org)
	}
	_, err := repo.conn.CliCommand(args...)
	return err
}
