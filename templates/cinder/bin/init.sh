#!/bin//bash
#
# Copyright 2020 Red Hat Inc.
#
# Licensed under the Apache License, Version 2.0 (the "License"); you may
# not use this file except in compliance with the License. You may obtain
# a copy of the License at
#
#      http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS, WITHOUT
# WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the
# License for the specific language governing permissions and limitations
# under the License.
set -ex

# This script generates the nova.conf/logging.conf file and
# copies the result to the ephemeral /var/lib/config-data/merged volume.
#
# Secrets are obtained from ENV variables.
export DB=${DatabaseName:-"cinder"}
export DBHOST=${DatabaseHost:?"Please specify a DatabaseHost variable."}
export DBUSER=${DatabaseUser:-"cinder"}
export DBPASSWORD=${DatabasePassword:?"Please specify a DatabasePassword variable."}
export CINDERPASSWORD=${CinderPassword:?"Please specify a CinderPassword variable."}
# TODO: nova password
#export NOVAPASSWORD=${NovaPassword:?"Please specify a NovaPassword variable."}
# TODO: transportURL
#export TRANSPORTURL=${TransportURL:?"Please specify a TransportURL variable."}

export VOLUMECUSTOMCONF=${VolumeCustomConf:-""}

SVC_CFG=/etc/cinder/cinder.conf
SVC_CFG_MERGED=/var/lib/config-data/merged/cinder.conf

# expect that the common.sh is in the same dir as the calling script
SCRIPTPATH="$( cd "$(dirname "$0")" >/dev/null 2>&1 ; pwd -P )"
. ${SCRIPTPATH}/common.sh --source-only

# Copy default service config from container image as base
cp -a ${SVC_CFG} ${SVC_CFG_MERGED}

# Merge all templates from config-data
for dir in /var/lib/config-data/default
do
    merge_config_dir ${dir}
done

# TODO: a cleaner way to handle this?
# Merge custom.conf with cinder.conf, since the Kolla config doesn't seem
# to allow us to customize the cinder command (it calls httpd instead).
# Can we just put custom.conf in something like /etc/cinder/cinder.conf.d/custom.conf
# and have it automatically detected, or would we have to somehow change the call
# to the cinder binary to tell it to use that custom conf dir?
echo merging /var/lib/config-data/default/custom.conf into /var/lib/config-data/merged/cinder.conf
crudini --merge /var/lib/config-data/merged/cinder.conf < /var/lib/config-data/default/custom.conf

# TODO: a cleaner way to handle this?
# If this is the init for a CinderVolume, there might be extra custom conf that
# needs to be merged with the main cinder.conf for this particular volume service
if [ -n "$VOLUMECUSTOMCONF" ]; then
  echo merging /var/lib/config-data/default/${VOLUMECUSTOMCONF} into /var/lib/config-data/merged/cinder.conf
  crudini --merge /var/lib/config-data/merged/cinder.conf < /var/lib/config-data/default/${VOLUMECUSTOMCONF}
fi

# set secrets
# TODO: transportURL
#crudini --set ${SVC_CFG_MERGED} DEFAULT transport_url $TransportURL
crudini --set ${SVC_CFG_MERGED} database connection mysql+pymysql://${DBUSER}:${DBPASSWORD}@${DBHOST}/${DB}
crudini --set ${SVC_CFG_MERGED} keystone_authtoken password $CINDERPASSWORD
# TODO: nova password
#crudini --set ${SVC_CFG_MERGED} nova password $NOVAPASSWORD
# TODO: service token
#crudini --set ${SVC_CFG_MERGED} service_user password $CinderPassword
