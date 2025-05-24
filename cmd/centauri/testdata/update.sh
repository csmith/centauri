#!/usr/bin/env bash
# Simple DNS challenge exec solver.
# Use challtestsrv https://github.com/letsencrypt/boulder/tree/master/test/challtestsrv
#
# This script is from the lego project, used under the following license:
#
# The MIT License (MIT)
#
# Copyright (c) 2017-2024 Ludovic Fernandez
# Copyright (c) 2015-2017 Sebastian Erhart
#
# Permission is hereby granted, free of charge, to any person obtaining a copy
# of this software and associated documentation files (the "Software"), to deal
# in the Software without restriction, including without limitation the rights
# to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
# copies of the Software, and to permit persons to whom the Software is
# furnished to do so, subject to the following conditions:
#
# The above copyright notice and this permission notice shall be included in all
# copies or substantial portions of the Software.
#
# THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
# IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
# FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
# AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
# LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
# OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
# SOFTWARE.

set -e

case "$1" in
  "present")
    echo  "Present"
    payload="{\"host\":\"$2\", \"value\":\"$3\"}"
    echo "payload=${payload}"
    curl -s -X POST -d "${payload}" localhost:8055/set-txt
    ;;
  "cleanup")
    echo  "cleanup"
    payload="{\"host\":\"$2\"}"
    echo "payload=${payload}"
    curl -s -X POST -d "${payload}" localhost:8055/clear-txt
    ;;
  *)
    echo "OOPS"
    ;;
esac