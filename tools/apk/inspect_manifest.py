#!/usr/bin/env python3
"""Offline, bounded APK identity inspection; does not execute APK code."""
import argparse, hashlib, json, struct, zipfile
from apk_safety import open_bounded_zip, write_json_output

MAX_MEMBER=16*1024*1024
NO_INDEX=0xffffffff

def u16(b,o): return struct.unpack_from('<H',b,o)[0]
def u32(b,o): return struct.unpack_from('<I',b,o)[0]
def chunks(b,start=0,end=None):
    end=len(b) if end is None else end
    pos=start
    while pos<end:
        if pos+8>end: raise ValueError('truncated chunk header')
        t,h,s=struct.unpack_from('<HHI',b,pos)
        if h<8 or s<h or pos+s>end: raise ValueError('invalid chunk boundary')
        yield t,h,s,pos
        pos+=s
    if pos!=end: raise ValueError('invalid chunk end')

def string_pool(b,pos,size):
    end=pos+size
    count=u32(b,pos+8); flags=u32(b,pos+16); base=pos+u32(b,pos+20)
    h=u16(b,pos+2)
    if count>1000000 or pos+h+count*4>end: raise ValueError('invalid pool count')
    result=[]
    def length8(o):
        n=b[o]; o+=1
        if n&128: n=((n&127)<<8)|b[o];o+=1
        return n,o
    def length16(o):
        n=u16(b,o);o+=2
        if n&32768:n=((n&32767)<<16)|u16(b,o);o+=2
        return n,o
    for i in range(count):
        o=base+u32(b,pos+h+i*4)
        if not base<=o<end:raise ValueError('string offset out of bounds')
        if flags&256:
            _,o=length8(o); n,o=length8(o)
            if o+n+1>end or b[o+n]!=0:raise ValueError('bad UTF8 pool string')
            s=b[o:o+n].decode('utf-8','strict')
        else:
            n,o=length16(o)
            if o+n*2+2>end or b[o+n*2:o+n*2+2]!=b'\0\0':raise ValueError('bad UTF16 pool string')
            s=b[o:o+n*2].decode('utf-16le','strict')
        result.append(s)
    return result

def parse_manifest(b):
    t,h,s,_=next(chunks(b))
    if t!=3 or s!=len(b):raise ValueError('not binary Android XML')
    pool=[]; stack=[]; root=None
    def ss(i):
        if i==NO_INDEX:return None
        if i>=len(pool):raise ValueError('bad string index')
        return pool[i]
    for t,h,s,o in chunks(b,h,len(b)):
        if t==1:pool=string_pool(b,o,s)
        if t==0x102:
            ext=o+16
            if h<16 or ext+20>o+s:raise ValueError('bad start element')
            name=ss(u32(b,ext+4)); astart,asz,cnt=struct.unpack_from('<HHH',b,ext+8)
            if asz<20 or ext+astart+cnt*asz>o+s:raise ValueError('bad attributes')
            attrs={}
            for i in range(cnt):
                a=ext+astart+i*asz
                ns=ss(u32(b,a)); n=ss(u32(b,a+4)); raw=ss(u32(b,a+8)); typ=b[a+15];data=u32(b,a+16)
                key=('android:' if ns=='http://schemas.android.com/apk/res/android' else '')+n
                value=ss(data) if typ==3 else bool(data) if typ==0x12 else data if typ in (0x10,0x11) else f'@0x{data:08x}' if typ==1 else f'type{typ}:0x{data:08x}'
                attrs[key]={'value':value,'raw':raw,'type':typ,'data':data}
            node={'tag':name,'attributes':attrs,'children':[]}
            if stack:stack[-1]['children'].append(node)
            elif root is None:root=node
            else:raise ValueError('multiple roots')
            stack.append(node)
        elif t==0x103:
            if not stack or o+24>o+s:raise ValueError('unbalanced end element')
            if ss(u32(b,o+20))!=stack[-1]['tag']:raise ValueError('mismatched end element')
            stack.pop()
    if stack or root is None:raise ValueError('unclosed or missing root')
    return root

def value(node,key):return node.get('attributes',{}).get(key,{}).get('value')
def resolve_label(b,resource):
    """Resolve simple string entries for one resource ID, returning configurations."""
    target=int(resource[1:],16); result=[]; gp=[]
    typ,header,size,_=next(chunks(b))
    if typ!=2 or size!=len(b):raise ValueError('not resources table')
    for t,h,s,o in chunks(b,header):
        if t==1:gp=string_pool(b,o,s)
        if t!=0x200:continue
        pid=u32(b,o+8)
        if pid!=target>>24:continue
        typeoffset=u32(b,o+284) if h>=288 else 0
        for ct,ch,cs,co in chunks(b,o+h,o+s):
            if ct!=0x201:continue
            tid=b[co+8];flags=b[co+9];count=u32(b,co+12);start=u32(b,co+16)
            if tid+typeoffset!=(target>>16)&255:continue
            idx=target&65535
            if flags:result.append({'unresolved':'sparse or offset16 entries'});continue
            if idx>=count:continue
            at=co+ch+idx*4
            if at+4>co+cs:raise ValueError('entry index out of bounds')
            entry=u32(b,at)
            if entry==NO_INDEX:continue
            p=co+start+entry
            if p+8>co+cs:raise ValueError('entry out of bounds')
            esz=u16(b,p);eflags=u16(b,p+2)
            if eflags&1:result.append({'unresolved':'complex entry'});continue
            q=p+esz
            if q+8>co+cs:raise ValueError('value out of bounds')
            vt=b[q+3];vd=u32(b,q+4)
            if vt==3 and vd<len(gp):result.append({'value':gp[vd], 'config_hex':b[co+20:co+ch].hex()})
            else:result.append({'unresolved':f'type {vt} data {vd}'})
    return result

def inspect(apk):
    with open_bounded_zip(apk) as z:
        names=z.namelist()
        def member(n):
            if names.count(n)!=1:raise ValueError('missing or duplicate identity member: '+n)
            i=z.getinfo(n)
            if i.file_size>MAX_MEMBER:raise ValueError('member too large: '+n)
            return z.read(i)
        manifest=member('AndroidManifest.xml');tree=parse_manifest(manifest)
        app=next(x for x in tree['children'] if x['tag']=='application')
        summary={k:value(tree,k) for k in ('package','android:versionCode','android:versionName')}
        summary['manifest_sha256']=hashlib.sha256(manifest).hexdigest()
        summary['application_class']=value(app,'android:name');summary['application_label']=value(app,'android:label')
        summary['application_debuggable']=value(app,'android:debuggable')
        summary['uses_sdk']=[{k:value(x,k) for k in ('android:minSdkVersion','android:targetSdkVersion','android:maxSdkVersion') if k in x['attributes']} for x in tree['children'] if x['tag']=='uses-sdk']
        permissions=[value(x,'android:name') for x in tree['children'] if x['tag']=='uses-permission']
        summary['permissions_count']=len(permissions)
        summary['declares_internet_permission']='android.permission.INTERNET' in permissions
        launchers=[];activity_count=0
        for x in app['children']:
            if x['tag'] not in ('activity','activity-alias'):continue
            activity_count+=1
            for f in x['children']:
                if f['tag']!='intent-filter':continue
                pairs=[(y['tag'],value(y,'android:name')) for y in f['children'] if y['tag'] in ('action','category')]
                if ('action','android.intent.action.MAIN') in pairs and ('category','android.intent.category.LAUNCHER') in pairs:
                    launchers.append({'name':value(x,'android:name'),'target':value(x,'android:targetActivity')})
                    break
        summary['launcher_activities']=launchers
        summary['activity_count']=activity_count
        if str(summary['application_label']).startswith('@0x'):
            arsc=member('resources.arsc');summary['resources_arsc_sha256']=hashlib.sha256(arsc).hexdigest();summary['resolved_label']=resolve_label(arsc,summary['application_label'])
        return summary

def main():
    a=argparse.ArgumentParser(description=__doc__)
    a.add_argument('--apk',required=True,help='Supplied APK to inspect offline')
    a.add_argument('--output',help='Optional JSON output file; JSON is always written to stdout')
    args=a.parse_args()
    try:
        result=inspect(args.apk)
        encoded=json.dumps(result,ensure_ascii=False,indent=2)+'\n'
        if args.output:write_json_output(args.apk,args.output,encoded)
    except (ValueError,IndexError,KeyError,StopIteration,struct.error,UnicodeError,OSError,RuntimeError,zipfile.BadZipFile) as e:
        a.exit(1,'Inspection failed: '+str(e)+'\n')
    print(encoded,end='')
if __name__=='__main__':main()
