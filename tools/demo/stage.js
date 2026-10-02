const layout = await (await fetch("/stage/layout.json")).json();
const query = new URLSearchParams(location.search);

const world = document.getElementById("world");
const windowNode = document.getElementById("window");
const pc = document.getElementById("pc");
const phoneNode = document.getElementById("phone");
const screen = phoneNode.querySelector(".screen");
const handset = document.getElementById("handset");
const frame = phoneNode.querySelector(".frame");
const touch = document.getElementById("touch");
const cursor = document.getElementById("cursor");
const ripple = document.getElementById("ripple");
const fade = document.getElementById("fade");

function place(node, { x, y, width, height }) {
  Object.assign(node.style, { left: `${x}px`, top: `${y}px`, width: `${width}px`, height: `${height}px` });
}

const { device } = layout;
place(windowNode, { ...layout.window, height: layout.window.height + layout.window.caption });
pc.width = layout.window.width;
pc.height = layout.window.height;
place(phoneNode, { x: layout.phone.x, y: layout.phone.y, ...device.size });
place(screen, device.screen);
screen.style.borderRadius = `${device.screen.radius}px`;
handset.width = device.viewport.width;
handset.height = device.viewport.height;
handset.style.transform = `scale(${device.screen.width / device.viewport.width})`;
if (device.image) {
  const image = document.createElement("img");
  image.src = device.image;
  image.alt = "";
  frame.append(image);
} else {
  frame.classList.add("placeholder");
}

const loaded = (node, src) =>
  new Promise((resolve) => {
    node.addEventListener("load", resolve, { once: true });
    node.src = src;
  });

function opacity(node, value) {
  node.style.opacity = String(Math.round(value * 10000) / 10000);
}

globalThis.__stage = {
  ready: Promise.all([loaded(pc, "/"), loaded(handset, `/${query.get("phone") ?? ""}`)]),
  apply(state) {
    const { camera } = state;
    const shiftX = layout.canvas.width / 2 - camera.x * camera.zoom;
    const shiftY = layout.canvas.height / 2 - camera.y * camera.zoom;
    world.style.transform = `translate(${shiftX}px, ${shiftY}px) scale(${camera.zoom})`;
    opacity(windowNode, state.window.opacity);
    phoneNode.style.transform = `translate(0, ${state.phone.offset}px) scale(${layout.phone.scale})`;
    opacity(phoneNode, state.phone.opacity);
    cursor.style.left = `${state.cursor.x}px`;
    cursor.style.top = `${state.cursor.y}px`;
    cursor.style.transform = `scale(${state.cursor.scale})`;
    opacity(cursor, state.cursor.opacity);
    ripple.style.left = `${state.ripple.x}px`;
    ripple.style.top = `${state.ripple.y}px`;
    ripple.style.transform = `scale(${state.ripple.scale})`;
    opacity(ripple, state.ripple.opacity);
    touch.style.left = `${state.touch.x}px`;
    touch.style.top = `${state.touch.y}px`;
    touch.style.transform = `scale(${state.touch.scale})`;
    opacity(touch, state.touch.opacity);
    opacity(fade, state.fade);
  },
};
