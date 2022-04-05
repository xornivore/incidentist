# incidentist

Incidentist, it's more fun than pulling teeth!

Group PD pages by incident from Datadog API with links and output in markdown, fancy!

## Usage

```shell
export PD_AUTH_TOKEN=...
export DD_API_KEY=...
export DD_APP_KEY=...
incidentist --team my-team --pd-team my_team --tags team:my-team --since 2021-07-14 --until 2021-07-27 --replace "/service-pod-.*/service-pod/" > ~/incidents.md
```
