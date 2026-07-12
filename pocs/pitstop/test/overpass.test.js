import { test } from "node:test";
import assert from "node:assert/strict";
import { buildQuery, parseShops, directionsUrl, MAX_RESULTS } from "../app/js/lib/overpass.js";

test("buildQuery embeds lat/lon/radius and both shop types", () => {
  const q = buildQuery(37.7749, -122.4194, 8000);
  assert.match(q, /around:8000,37.7749,-122.4194/g);
  assert.match(q, /"shop"="car_repair"/);
  assert.match(q, /"shop"="tyres"/);
  assert.match(q, /node\["shop"="car_repair"\]/);
  assert.match(q, /way\["shop"="car_repair"\]/);
});

const origin = { lat: 37.7749, lon: -122.4194 };

test("parseShops extracts node coords and tags", () => {
  const json = {
    elements: [
      { type: "node", id: 1, lat: 37.775, lon: -122.4195, tags: { name: "Joe's Tires", shop: "tyres" } },
    ],
  };
  const shops = parseShops(json, origin);
  assert.equal(shops.length, 1);
  assert.equal(shops[0].name, "Joe's Tires");
  assert.equal(shops[0].kind, "Tire shop");
  assert.equal(shops[0].id, "node/1");
  assert.ok(shops[0].distanceMi >= 0);
});

test("parseShops uses way center and falls back to brand/unnamed", () => {
  const json = {
    elements: [
      { type: "way", id: 2, center: { lat: 37.78, lon: -122.41 }, tags: { shop: "car_repair", brand: "Midas" } },
      { type: "way", id: 3, center: { lat: 37.79, lon: -122.42 }, tags: { shop: "car_repair" } },
    ],
  };
  const shops = parseShops(json, origin);
  assert.equal(shops.length, 2);
  assert.ok(shops.some((s) => s.name === "Midas"));
  assert.ok(shops.some((s) => s.name === "Unnamed shop"));
  for (const s of shops) assert.equal(s.kind, "Auto repair");
});

test("parseShops composes an address from addr:* tags when present", () => {
  const json = {
    elements: [
      {
        type: "node",
        id: 4,
        lat: 37.78,
        lon: -122.41,
        tags: { name: "Bay Tires", shop: "tyres", "addr:housenumber": "100", "addr:street": "Main St", "addr:city": "SF" },
      },
    ],
  };
  const shops = parseShops(json, origin);
  assert.equal(shops[0].address, "100 Main St, SF");
});

test("parseShops skips elements without resolvable coordinates", () => {
  const json = { elements: [{ type: "way", id: 5, tags: { shop: "tyres", name: "No Center" } }] };
  assert.equal(parseShops(json, origin).length, 0);
});

test("parseShops sorts by distance ascending", () => {
  const json = {
    elements: [
      { type: "node", id: 10, lat: 38.5, lon: -122.4, tags: { name: "Far", shop: "tyres" } },
      { type: "node", id: 11, lat: 37.775, lon: -122.4195, tags: { name: "Near", shop: "tyres" } },
    ],
  };
  const shops = parseShops(json, origin);
  assert.equal(shops[0].name, "Near");
  assert.equal(shops[1].name, "Far");
});

test("parseShops dedupes by name + rounded coordinates", () => {
  const json = {
    elements: [
      { type: "node", id: 20, lat: 37.775, lon: -122.4195, tags: { name: "Dup Tires", shop: "tyres" } },
      { type: "way", id: 21, center: { lat: 37.775, lon: -122.4195 }, tags: { name: "Dup Tires", shop: "tyres" } },
    ],
  };
  assert.equal(parseShops(json, origin).length, 1);
});

test("parseShops caps results at MAX_RESULTS", () => {
  const elements = [];
  for (let i = 0; i < MAX_RESULTS + 10; i += 1) {
    elements.push({
      type: "node",
      id: i,
      lat: 37.7749 + i * 0.001,
      lon: -122.4194,
      tags: { name: `Shop ${i}`, shop: "car_repair" },
    });
  }
  const shops = parseShops({ elements }, origin);
  assert.equal(shops.length, MAX_RESULTS);
});

test("directionsUrl builds a Google Maps link to the shop coords", () => {
  const url = directionsUrl({ lat: 37.78, lon: -122.41 });
  assert.equal(url, "https://www.google.com/maps/dir/?api=1&destination=37.78,-122.41");
});
