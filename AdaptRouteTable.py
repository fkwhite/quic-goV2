#!/usr/bin/env python3
# encoding: utf-8

import os
import sys
import argparse
parser = argparse.ArgumentParser(description='Change routing table by setting destination/gateway/interface')
parser.add_argument('-d', '--dest_net', type=str, help='Destination network address', required=True)
parser.add_argument('-g', '--gateway', type=str, help='Gateway/next-hop', required=True)
parser.add_argument('-i', '--interface', type=str, help='Interface', required=True)


def main():
  '''
  '''
  args = parser.parse_args()
  cmd = 'ip route add {} via {} dev {}'.format(args.dest_net, args.gateway, args.interface)
  print(cmd)
  os.system(cmd)
  print('ip route show')


if __name__ == '__main__':
  main()