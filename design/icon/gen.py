import math
# macOS Big Sur grid: 1024 canvas, 824 body at offset 100, superellipse corners
def squircle(x0,y0,s,n=5.0,steps=360):
    cx,cy,r=x0+s/2,y0+s/2,s/2
    pts=[]
    for i in range(steps):
        t=2*math.pi*i/steps
        c,si=math.cos(t),math.sin(t)
        x=cx+r*math.copysign(abs(c)**(2/n),c)
        y=cy+r*math.copysign(abs(si)**(2/n),si)
        pts.append(f"{x:.2f},{y:.2f}")
    return "M"+" L".join(pts)+" Z"
BODY=squircle(100,100,824)
COMMON=f'''<defs>
<clipPath id="body"><path d="{BODY}"/></clipPath>
<filter id="drop" x="-20%" y="-20%" width="140%" height="140%">
 <feDropShadow dx="0" dy="12" stdDeviation="14" flood-color="#000" flood-opacity="0.28"/></filter>
<filter id="blur80"><feGaussianBlur stdDeviation="80"/></filter>
<filter id="soft" x="-30%" y="-30%" width="160%" height="160%">
 <feDropShadow dx="0" dy="18" stdDeviation="22" flood-color="#2a2060" flood-opacity="0.25"/></filter>
<linearGradient id="sheen" x1="0" y1="0" x2="0" y2="1">
 <stop offset="0" stop-color="#fff" stop-opacity=".95"/><stop offset="1" stop-color="#fff" stop-opacity=".62"/></linearGradient>
<linearGradient id="rim" x1="0" y1="0" x2="1" y2="1">
 <stop offset="0" stop-color="#fff" stop-opacity="1"/><stop offset="1" stop-color="#fff" stop-opacity=".25"/></linearGradient>
</defs>'''
def aurora(bg, a, b, c, op=1):
    return f'''<g clip-path="url(#body)">
<rect x="100" y="100" width="824" height="824" fill="{bg}"/>
<g filter="url(#blur80)" opacity="{op}">
 <circle cx="230" cy="250" r="300" fill="{a}"/>
 <circle cx="830" cy="230" r="300" fill="{b}"/>
 <circle cx="560" cy="900" r="320" fill="{c}"/>
</g></g>'''
# N glyph: geometric, rounded joins
def N(cx,cy,h,w,sw,color):
    x0,x1=cx-w/2,cx+w/2; y0,y1=cy-h/2,cy+h/2
    return f'<path d="M{x0},{y1} L{x0},{y0} L{x1},{y1} L{x1},{y0}" fill="none" stroke="{color}" stroke-width="{sw}" stroke-linecap="round" stroke-linejoin="round"/>'
def wrap(inner):
    return f'<svg xmlns="http://www.w3.org/2000/svg" width="1024" height="1024" viewBox="0 0 1024 1024">{COMMON}<g filter="url(#drop)"><path d="{BODY}" fill="#fff"/></g>{inner}</svg>'

# A: light aurora + glass page with folded corner + N
page=("M330,250 h270 l110,110 v384 a40,40 0 0 1 -40,40 h-340 a40,40 0 0 1 -40,-40 v-454 a40,40 0 0 1 40,-40 Z")
fold="M600,250 v80 a30,30 0 0 0 30,30 h80 Z"
A=wrap(aurora("#f4f3fb","#7f97ff","#ff8cc8","#7af0c4")+f'''
<g filter="url(#soft)"><path d="{page}" fill="url(#sheen)"/></g>
<path d="{page}" fill="none" stroke="url(#rim)" stroke-width="4"/>
<path d="{fold}" fill="#fff" opacity=".9"/>
{N(510,560,250,180,58,"#1d1d1f")}''')

# B: dark, aurora glow, white N in glass tile
B=wrap(aurora("#0f0f14","#4b5cff","#ff4fa3","#2fd6a0",.85)+f'''
<g clip-path="url(#body)"><rect x="100" y="100" width="824" height="824" fill="#0b0b10" opacity=".35"/></g>
<rect x="262" y="262" width="500" height="500" rx="120" fill="#fff" fill-opacity=".12" stroke="#fff" stroke-opacity=".35" stroke-width="4"/>
{N(512,512,240,190,62,"#ffffff")}''')

# C: arch "nook" alcove with a page inside
arch="M292,800 V470 a220,220 0 0 1 440,0 V800 Z"
C=wrap(aurora("#f6f4fa","#8aa2ff","#ff9bd0","#86f2cf")+f'''
<path d="{arch}" fill="#1d1d1f"/>
<g filter="url(#soft)"><rect x="392" y="455" width="240" height="300" rx="28" fill="url(#sheen)"/></g>
<rect x="430" y="510" width="150" height="20" rx="10" fill="#1d1d1f" opacity=".85"/>
<rect x="430" y="560" width="164" height="14" rx="7" fill="#1d1d1f" opacity=".35"/>
<rect x="430" y="596" width="130" height="14" rx="7" fill="#1d1d1f" opacity=".35"/>
<rect x="430" y="632" width="150" height="14" rx="7" fill="#1d1d1f" opacity=".35"/>''')
for name,svg in (("Nook",A),("Nook-dark",B),("Nook-arch",C)):
    open(f"{name}.svg","w").write(svg)

