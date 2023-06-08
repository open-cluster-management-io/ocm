# Changelog 
All notable changes to this project will be documented in this file.

## v0.7.0

### New Features
* Support override configuration of mca from cma. ([#151](https://github.com/open-cluster-management-io/addon-framework/pull/151) [@qiujian16](https://github.com/qiujian16))
* Support Rollout/Canary upgrade for addon. (
    [#163](https://github.com/open-cluster-management-io/addon-framework/pull/163)
    [#164](https://github.com/open-cluster-management-io/addon-framework/pull/164)
    [#170](https://github.com/open-cluster-management-io/addon-framework/pull/170)
    [#176](https://github.com/open-cluster-management-io/addon-framework/pull/176)
    [#179](https://github.com/open-cluster-management-io/addon-framework/pull/179)
    [#180](https://github.com/open-cluster-management-io/addon-framework/pull/180)
    [#182](https://github.com/open-cluster-management-io/addon-framework/pull/182)
    [@haoqing0110](https://github.com/haoqing0110)
)

### Added
* Add doc for addon deployment config. ([#128](https://github.com/open-cluster-management-io/addon-framework/pull/128) [@skeeey](https://github.com/skeeey))
* Add golangci lint config file. ([#161](https://github.com/open-cluster-management-io/addon-framework/pull/161) [@zhujian7](https://github.com/zhujian7))
* Add config-spec-hash annotation to manifestwork. ([#157](https://github.com/open-cluster-management-io/addon-framework/pull/157) [@haoqing0110](https://github.com/haoqing0110))
* Add namespace in addon status. ([#168](https://github.com/open-cluster-management-io/addon-framework/pull/168) [@qiujian16](https://github.com/qiujian16))
* Add addon filter for addonmanager. ([#177](https://github.com/open-cluster-management-io/addon-framework/pull/177) [@zhiweiyin318](https://github.com/zhiweiyin318))
* Make manifestWork UpdateStrategy configurable through AgentAddonOptions. ([#178](https://github.com/open-cluster-management-io/addon-framework/pull/178) [@fgiloux](https://github.com/fgiloux))
* Add check api server health func. ([#181](https://github.com/open-cluster-management-io/addon-framework/pull/181) [@zhiweiyin318](https://github.com/zhiweiyin318))

### Changes
* Use cond/finalizer in api repo. ([#169](https://github.com/open-cluster-management-io/addon-framework/pull/169) [@qiujian16](https://github.com/qiujian16))
* Start addon status update in addon's own manager. ([#166](https://github.com/open-cluster-management-io/addon-framework/pull/166) [@qiujian16](https://github.com/qiujian16))
* Upgrade helm to 3.11.1. ([#174](https://github.com/open-cluster-management-io/addon-framework/pull/174) [@zhujian7](https://github.com/zhujian7))
* Refactor move healthcheck to agentdeploy controller. ([#172](https://github.com/open-cluster-management-io/addon-framework/pull/172) [@zhiweiyin318](https://github.com/zhiweiyin318))
* Enhance RegistrationApplied condition. ([#175](https://github.com/open-cluster-management-io/addon-framework/pull/175) [@haoqing0110](https://github.com/haoqing0110))

### Bug Fixes
* Fix gosec implicit memory aliasing in for loop. ([#160](https://github.com/open-cluster-management-io/addon-framework/pull/160) [@zhiweiyin318](https://github.com/zhiweiyin318))
* Only enable install strategy and update install progression when addon is managed by addon-manager. ([#184](https://github.com/open-cluster-management-io/addon-framework/pull/184) [@haoqing0110](https://github.com/haoqing0110))

### Removed & Deprecated
* Remove deprecated finalizer ([#171](https://github.com/open-cluster-management-io/addon-framework/pull/171) [@qiujian16](https://github.com/qiujian16))