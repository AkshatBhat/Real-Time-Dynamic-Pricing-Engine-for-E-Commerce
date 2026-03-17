#!/bin/bash
set -e

sqlplus -s pricing/pricingpass@//localhost:1521/FREEPDB1 @/container-entrypoint-initdb.d/01_create_tables.sql