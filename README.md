## Pitia - Real-Time & Highly Scalable Recommender System 

![Pitia](https://raw.githubusercontent.com/alonsovidales/pit/master/static/img/favicon.png)

### Description
Pitia is an open source recommender system developed using Go and and based in an improved version of the algorithm described in the "Adaptive Bootstrapping of Recommender Systems Using Decision Trees" paper by Yahoo.

After test the recomendations algorithm we got more than a 95% of precision using the Netflix Prize dataset, you can read more about how the tests was performed on [our blog](http://blog.pitia.info/2015/06/more-than-95-of-precision-obtained.html).

Pitia provides an easy to use [HTTP API](http://pitia.info/api) that can be integrated on almost any client.

This project is designed as a horizontally scalable system based on the concept of virtual shards inside instances. The system was designed to be deployed on an array of instances behind a load balancer in order to distribute randomly the requests across the nodes. There is no a master instance in the cluster and all the new instances are registered automatically being registered, so to scale the system just add new instances, is recomended to add autoscaling based in the CPU and memory usage of the nodes.

Dynamo DB is used to coordinate the virtual shards distribution, architecture of the cluster, and to store the accounts information.

#### Intances, groups, shards and accounts
The system contains *User Accounts*, each user account contains *Groups* and each group, *Virtual Shards*

Each group has to be used for a single purpose, for instance we can have groups to perform recommendations of movies, books, etc. Each different use case has to be isolated in a separate group.

For instance, a group can store book classifications in order to be used to perform recommendations of books based on the books that the users had been read, but other group can contain items of a store in general, in order to perform recommendations of items to buy based in the elements that the user had bought before.

Each group contains number of Virtual Shards, up to the number of available instances, defined by the user, since each shard is going to be allocated in a different inscante, the shards can be of different types (see [Pricing section](http://pitia.info/pricing)) , the type will define the number of requests per second and elements that can be stored on each shard, this properties can be configured in the INI file.

Since each shard is allocated in a different node in case of one of the nodes goes down the shards allocated by this node are going to be acquired by another nodes. In order to grant high availability, it is not recommended to define less than two shards by group.

#### Shard adquisition
In order to distribute the shards across the cluster instances, the system uses a bidding strategy, each instance try to acquire a group if have enough resources to allocate it, to bid for a shard, the instance inscribes itself in a DynamoDB table, after some seconds, time enough for the other instances to claim that shard, the shard with more resources available that is claiming this shard will acquire it.

If an instance goes down, the shards are released after a period of time that can be defined in the INI config file being them released, and the other nodes are going to start with the bidding strategy to claim this free shards.

The information stored on each shard is not shared with another shards of the same group since the purpose of this system is to perform recommendations and based in the idea that the load balancer is going to distribute randomly the incoming requests across all the available instances we can consider that the quality of the predictions is the same for all the shards.

#### Data storage
Each shard is going to dump all the information in memory periodically into S3 encoded as JSON, and each time a new shard is adquired the memory will be restored using the last available backup on S3.

### Installation and configuration
The configuration of each of the cluster nodes is defined in two places, the /etc/pit_\<env>.ini file, and some environment variables, the INI file contains the most general configuration parameters and this file can be upload to any public repository without security risks, the environment variables contains security related variables.
The environment variables to be present on the system are the next:

```
AWS_DEFAULT_REGION = AWS region where the S3 and DynamoDB information will be stored 
AWS_ACCESS_KEY_ID = AWS AMI key ID , this key has to have access to the S3 bucked and to DynamoDB
AWS_SECRET_ACCESS_KEY = AWS AMI key
PIT_MAIL_PASS = Password of the e-mail used to send notifications
```

#### Deployment
There is a MakeFile that will help you out with the most common tasks like:

* **deps:** Downloads and installs all the Go reqiered dependencies
* **updatedeps:** In case of have the dependencies already installed, this script will update them to the last available version, is recommended to use GoDeps in order to avoid problems with versions, etc, this scripts are designed to help during the developement process
* **format:** Goes through all the files using "go fmt" auto formating them
* **test:** Lauches all the test suites for all the packages
* **deb:** Compiles the aplication for amd64 architecture building a debian package that can be used to install the aplication on all the environments
* **static_deb:** Generates a debian package that contains all the static content used by the https://wwww.pitia.info website and contained in the *static* directory.
* **deploy_dev:** Generates the debian package using the "deb" script, uploads and deploys it into all the machines specified on the env var PIT_DEV_SERVERS , use spaces as separator for the machine names, like export PIT_DEV_SERVERS="machine1 machihne2 ... machineN"
* **deploy_pro:** Generates the debian package using the "deb" script, uploads and deploys it into all the machines specified on the env var PIT_PRO_SERVERS , use spaces as separator for the machine names, like export PIT_PRO_SERVERS="machine1 machihne2 ... machineN"
* **deploy_static_pro:** Generates the debian package for static content only using the "static_deb" script and uploads and deploys it into all the machines specified on the env var PIT_PRO_SERVERS , use spaces as separator for the machine names, like export PIT_PRO_SERVERS="machine1 machihne2 ... machineN"

In order to start the system after install it in a node, execute:
```
pit <env>
```
Where env is the corresponding name for the environment, this var will define what file to use of the available INI files on the machine, for instance, *pit pro* will get the configuration from */etc/pit_pro.ini*

### Management
The system can be managed from the Web panel, this panel provides a high level management, and allows to:

* Create accounts
* Update account information
* Retrieve account passwords
* Verify account identity
* Create groups of shards
* Configure groups of shards
* Visualize the current status of the shards, elements stored on each one, load that are receiving, status, etc.
* Regenerate the secret key for each group
* Access to the account logs to know all the different changes that had been performed on the user account
* Access to the billing information

![Panel](http://pitia.info/img/manage_pannel.png)

A lower level management can be performed using the *pit-cli* terminal client, in order to use this client is necessary to have the environment configured on the machine where it is going to be executed, this means the corresponding INI file and environment variables.

This client provides a super user access, that means that you can work with any account registered on the system, and allows to perform the next operations:

* List all the instances on the cluster
* List all the registered users
* Add new user accounts
* Show all the information regarding to a user
* Enable / disable user accounts
* List all the available groups of shards in the cluster
* Remove groups of shards
* Change the configuration for a group of shards

```
root@pit-pro-004:~# pit-cli --env pro --help
usage: pit-cli --env=ENV <command> [<flags>] [<args> ...]

Pit-cli is a tool to manage all the different features of the recommener system Pit

Flags:
  --help     Show help.
  --env=ENV  Configuration environment: pro, pre, dev

Commands:
  help [<command>]
    Show help for a command.

  instances list
    Lists all the instances on the clusted

  users list
    lists all the users

  users add <user-ID> <key>
    Adds a new user

  users show <user-ID>
    Shows all the sotred information for a specific user

  users enable <user-ID>
    Enables a disabled user account

  users disable <user-ID>
    Disables an enabled user account

  groups list [<flags>]
    lists all the shards in the system

  groups del <group-id>
    Removes one of the groups

  groups update --max-score=MAX-SCORE --num-shards=NUM-SHARDS --num-elems=NUM-ELEMS --max-req-sec=MAX-REQ-SEC --max-ins-req-sec=MAX-INS-REQ-SEC --user-id=USER-ID --group-id=GROUP-ID
    Adds or updates an existing shard
 ```

### License

Use of this source code is governed by the [GPL license](https://github.com/alonsovidales/pit/blob/master/license.txt). These programs and documents are distributed without any warranty, express or implied. All use of these programs is entirely at the user's own risk.











