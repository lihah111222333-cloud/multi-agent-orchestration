import random
import math
import os

OUTPUT_DIR = "/Users/mima0000/Desktop/Project_Logos"
os.makedirs(OUTPUT_DIR, exist_ok=True)

def random_color():
    return f"#{random.randint(0, 255):02x}{random.randint(0, 255):02x}{random.randint(0, 255):02x}"

def random_gradient(id):
    c1 = random_color()
    c2 = random_color()
    return f'''
    <linearGradient id="{id}" x1="0%" y1="0%" x2="100%" y2="100%">
      <stop offset="0%" style="stop-color:{c1};stop-opacity:1" />
      <stop offset="100%" style="stop-color:{c2};stop-opacity:1" />
    </linearGradient>
    '''

def save_svg(name, content, defs=""):
    svg = f'''<svg width="512" height="512" viewBox="0 0 512 512" xmlns="http://www.w3.org/2000/svg">
      <defs>{defs}</defs>
      <rect width="512" height="512" rx="50" fill="#1a1a1a" />
      {content}
    </svg>'''
    with open(os.path.join(OUTPUT_DIR, f"{name}.svg"), "w") as f:
        f.write(svg)

# --- Generators ---

def gen_geometric(idx):
    # Concentric or overlapping shapes
    content = ""
    defs = random_gradient(f"grad{idx}")
    for i in range(5):
        r = 150 - i * 20
        stroke_width = random.randint(2, 10)
        opacity = 1.0 - (i * 0.15)
        if i % 2 == 0:
             content += f'<circle cx="256" cy="256" r="{r}" fill="none" stroke="url(#grad{idx})" stroke-width="{stroke_width}" opacity="{opacity}" />'
        else:
             content += f'<rect x="{256-r}" y="{256-r}" width="{r*2}" height="{r*2}" fill="none" stroke="url(#grad{idx})" stroke-width="{stroke_width}" transform="rotate({i*15} 256 256)" opacity="{opacity}" />'
    save_svg(f"cortex_var_{idx}_geometric", content, defs)

def gen_cyber(idx):
    # Circuit paths
    content = ""
    defs = random_gradient(f"grad{idx}")
    content += f'<path d="M256 100 L256 412" stroke="url(#grad{idx})" stroke-width="4" />'
    content += f'<circle cx="256" cy="256" r="60" fill="none" stroke="url(#grad{idx})" stroke-width="8" />'
    for i in range(8):
        angle = i * (360/8)
        rad = math.radians(angle)
        x1 = 256 + 60 * math.cos(rad)
        y1 = 256 + 60 * math.sin(rad)
        x2 = 256 + 180 * math.cos(rad)
        y2 = 256 + 180 * math.sin(rad)
        content += f'<line x1="{x1}" y1="{y1}" x2="{x2}" y2="{y2}" stroke="url(#grad{idx})" stroke-width="4" />'
        content += f'<circle cx="{x2}" cy="{y2}" r="10" fill="url(#grad{idx})" />'
    save_svg(f"cortex_var_{idx}_cyber", content, defs)

def gen_abstract(idx):
    # Bezier curves
    content = ""
    defs = random_gradient(f"grad{idx}")
    for i in range(5):
        d = f"M {random.randint(50, 150)} {random.randint(100, 400)} Q {random.randint(100, 400)} {random.randint(0, 512)} {random.randint(350, 450)} {random.randint(100, 400)}"
        content += f'<path d="{d}" fill="none" stroke="url(#grad{idx})" stroke-width="{random.randint(5, 20)}" stroke-linecap="round" opacity="0.7" />'
    save_svg(f"cortex_var_{idx}_abstract", content, defs)

def gen_minimal(idx):
    # Simple C shape variations
    defs = random_gradient(f"grad{idx}")
    content = f'<path d="M350 150 A 150 150 0 1 0 350 362" fill="none" stroke="url(#grad{idx})" stroke-width="{random.randint(20, 50)}" stroke-linecap="round" />'
    content += f'<circle cx="350" cy="150" r="{random.randint(10, 30)}" fill="url(#grad{idx})" />'
    content += f'<circle cx="350" cy="362" r="{random.randint(10, 30)}" fill="url(#grad{idx})" />'
    save_svg(f"cortex_var_{idx}_minimal", content, defs)

def gen_pixel(idx):
    # Grid of squares
    defs = random_gradient(f"grad{idx}")
    content = ""
    for x in range(8):
        for y in range(8):
            if random.random() > 0.4:
                size = 40
                gap = 10
                start_x = 256 - (4 * (size+gap))
                start_y = 256 - (4 * (size+gap))
                px = start_x + x * (size+gap)
                py = start_y + y * (size+gap)
                content += f'<rect x="{px}" y="{py}" width="{size}" height="{size}" fill="url(#grad{idx})" opacity="{random.random()}" rx="5" />'
    save_svg(f"cortex_var_{idx}_pixel", content, defs)

# --- Main ---
generators = [gen_geometric, gen_cyber, gen_abstract, gen_minimal, gen_pixel]

for i in range(1, 21):
    gen_func = generators[(i-1) % len(generators)]
    gen_func(i)

print("Generated 20 logo variations.")
