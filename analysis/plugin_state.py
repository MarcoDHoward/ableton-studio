"""Decode Arturia Analog Lab V state blobs (boost text archive) stored in an .als.

Adds to each PluginDevice in set.json: preset, pack, type/subtype, tags,
description, and engine parameters (name -> 0..1 value).

usage: python plugin_state.py <set.xml> <set.json>
"""
import binascii, json, re, sys
import xml.etree.ElementTree as ET

def lp_strings(s):
    """Tokens of a boost text archive where strings are '<len> <chars>'."""
    i, n = 0, len(s)
    while i < n:
        m = re.compile(r'(\d+) ').match(s, i)
        if not m:
            i += 1; continue
        ln = int(m.group(1)); j = m.end()
        yield m.start(), s[j:j + ln], j + ln
        i = j + ln if ln else j

def decode(blob):
    s = blob.decode('latin1')
    out = {}
    h = s.find('serialization::archive')
    for m in re.finditer(r'(\d+) ', s[h:h + 120]):
        a = h + m.end(); ln = int(m.group(1)); name = s[a:a + ln]
        m2 = re.match(r' (\d+) ', s[a + ln:])
        if ln and m2:
            pl = int(m2.group(1)); b = a + ln + m2.end()
            if s[b + pl:b + pl + 4] == ' 60 ':
                out['preset'], out['pack'] = name, s[b:b + pl]
                m3 = re.match(r'(\d+) ', s[b + pl + 4:])
                if m3:
                    c = b + pl + 4 + m3.end(); out['author'] = s[c:c + int(m3.group(1))]
                break
    meta = {}
    for key in ('OriginalPresetName', 'OriginalPackName', 'Characteristics', 'Subtype', 'Type',
                'InstrumentPart1', 'InstVersionPart1'):
        mm = re.search(r'\d+ %s (\d+) ' % key, s)
        if mm:
            ln = int(mm.group(1)); meta[key] = s[mm.end():mm.end() + ln]
    out['meta'] = meta
    for d in re.finditer(r' 1 0 (\d+) ', s[:4000]):
        ln = int(d.group(1)); txt = s[d.end():d.end() + ln]
        if ln > 20 and re.match(r' \d{9,10} ', s[d.end() + ln:]):
            out['description'] = txt; break
    tags = {}
    for part in meta.get('Characteristics', '').split(';'):
        if ',' in part:
            k, v = part.split(',', 1); tags[k] = v.split('|')
    out['tags'] = tags
    params = {}
    for mm in re.finditer(r'(?<![\w.])(\d+) ([A-Za-z][\w.]*) (-?\d+(?:\.\d+)?(?:e-?\d+)?)(?= )', s):
        if int(mm.group(1)) == len(mm.group(2)):
            params[mm.group(2)] = float(mm.group(3))
    out['engine_params'] = params
    return out

def main(xml_path, json_path):
    root = ET.parse(xml_path).getroot()
    states = {}
    for t in root.find('LiveSet/Tracks'):
        name = t.find('Name/EffectiveName').get('Value')
        for k, pd in enumerate(t.iter('PluginDevice')):
            for buf in pd.iter():
                if buf.tag == 'ProcessorState' and buf.text and len(buf.text.strip()) > 100:
                    states[(name, k)] = decode(binascii.unhexlify(re.sub(r'\s+', '', buf.text)))
    o = json.load(open(json_path, encoding='utf-8'))
    for t in o['tracks']:
        k = 0
        def visit(devs):
            nonlocal k
            for d in devs:
                if d['type'] == 'PluginDevice':
                    st = states.get((t['name'], k)); k += 1
                    if st: d['state'] = st
                for ch in d.get('chains', []):
                    visit(ch['devices'])
        visit(t['devices'])
    json.dump(o, open(json_path, 'w', encoding='utf-8'), indent=1)
    for (tn, k), st in states.items():
        print('%-16s preset=%-24r type=%s/%s pack=%s inst=%s nparams=%d' % (
            tn, st.get('preset'), st['meta'].get('Type'), st['meta'].get('Subtype'),
            st['meta'].get('OriginalPackName'), st['meta'].get('InstrumentPart1'), len(st['engine_params'])))
        print('   tags', st['tags'])
        print('   desc', (st.get('description') or '')[:160])

if __name__ == '__main__':
    main(sys.argv[1], sys.argv[2])
