#!/usr/bin/env python3
"""Read DEX table metadata and curated identity refs, never code execution."""
import argparse,hashlib,json,struct,zipfile
from apk_safety import open_bounded_zip, write_json_output
MAX_DEX=16*1024*1024

def u32(b,o):return struct.unpack_from('<I',b,o)[0]
def uleb(b,o):
 n=0
 for shift in range(0,35,7):
  if o>=len(b):raise ValueError('truncated uleb')
  v=b[o];o+=1;n|=(v&127)<<shift
  if not v&128:return n,o
 raise ValueError('uleb overflow')
def span(b,o,n,stride):
 if n>1000000 or o<0 or o+n*stride>len(b):raise ValueError('invalid table bounds')
 return range(n)
def dex_info(b):
 if not b.startswith(b'dex\n') or len(b)<112 or u32(b,32)!=len(b):raise ValueError('bad dex size/magic')
 if u32(b,40)!=0x12345678:raise ValueError('unsupported endian')
 sn,so=u32(b,56),u32(b,60);tn,to=u32(b,64),u32(b,68);cn,co=u32(b,96),u32(b,100)
 strings=[]
 for i in span(b,so,sn,4):
  p=u32(b,so+i*4);_,p=uleb(b,p);end=b.find(b'\0',p)
  if end<0:raise ValueError('unterminated string')
  strings.append(b[p:end].decode('utf-8','replace'))
 types=[]
 for i in span(b,to,tn,4):
  idx=u32(b,to+i*4)
  if idx>=len(strings):raise ValueError('type string out of bounds')
  types.append(strings[idx])
 classes=[]
 for i in span(b,co,cn,32):
  idx=u32(b,co+i*32)
  if idx>=len(types):raise ValueError('class type out of bounds')
  classes.append(types[idx])
 prefixes=['Lcom/tencent/xjcard/','Lcom/tencent/gcloud/msdk/','Lcom/tencent/msdk/','Lcom/tencent/midas/','Lcom/tencent/midas/comm/','Lorg/cocos2dx/','Lcom/tencent/bugly/','Lcom/tencent/mm/','Lcom/tencent/mobileqq/']
 exact=['Lcom/tencent/xjcard/MainApplication;','Lcom/tencent/xjcard/xjcard;','Lcom/tencent/gcloud/msdk/core/policy/MSDKPolicyActivity;','Lorg/cocos2dx/lib/Cocos2dxActivity;','Lorg/cocos2dx/lib/Cocos2dxRenderer;','Lcom/tencent/midas/api/APMidasPayAPI;','Lcom/tencent/msdk/api/WGPlatform;']
 out={'dex_version':b[4:7].decode(),'sha256':hashlib.sha256(b).hexdigest(),'strings_count':sn,'types_count':tn,'class_defs_count':cn,
 'namespaces':[{'prefix':p,'type_refs':sum(x.startswith(p) for x in types),'class_defs':sum(x.startswith(p) for x in classes)} for p in prefixes],
 'selected_types':[{'descriptor':x,'type_reference':x in types,'class_definition':x in classes} for x in exact]}
 out['selected_namespace_classes']={p:sorted(x for x in classes if x.startswith(p))[:8] for p in prefixes[:4]+prefixes[5:6]}
 out['application_class_hierarchy'] = [{'class':types[u32(b,co+i*32)],'superclass':types[u32(b,co+i*32+8)] if u32(b,co+i*32+8)!=0xffffffff else None} for i in range(cn) if types[u32(b,co+i*32)] in ('Lcom/tencent/xjcard/xjcard;','Lcom/tencent/xjcard/MainApplication;','Lorg/cocos2dx/lib/Cocos2dxActivity;')]
 return out

def inspect(apk):
 with open_bounded_zip(apk) as z:
  name='classes.dex'
  if z.namelist().count(name)!=1:raise ValueError('missing or duplicate classes.dex')
  i=z.getinfo(name)
  if i.file_size>MAX_DEX:raise ValueError('large dex')
  return dex_info(z.read(i))

def main():
 a=argparse.ArgumentParser(description=__doc__)
 a.add_argument('--apk',required=True,help='Supplied APK to inspect offline')
 a.add_argument('--output',help='Optional JSON output file; JSON is always written to stdout')
 args=a.parse_args()
 try:
  result=inspect(args.apk)
  encoded=json.dumps(result,ensure_ascii=False,indent=2)+'\n'
  if args.output:write_json_output(args.apk,args.output,encoded)
 except (ValueError,IndexError,KeyError,struct.error,UnicodeError,OSError,RuntimeError,zipfile.BadZipFile) as e:
  a.exit(1,'Inspection failed: '+str(e)+'\n')
 print(encoded,end='')
if __name__=='__main__':main()
