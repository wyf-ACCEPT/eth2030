#!/usr/bin/env python3
"""
Fetch EIP/ERC discussion topics from ethereum-magicians.org (Discourse API).

Usage:
    python3 tools/fetch-magicians.py                    # fetch all categories
    python3 tools/fetch-magicians.py --category eips    # fetch only EIPs
    python3 tools/fetch-magicians.py --category ercs    # fetch only ERCs
    python3 tools/fetch-magicians.py --search "verkle"  # search for keyword
    python3 tools/fetch-magicians.py --limit 50         # limit topics per category

Output goes to data/magicians/<category>/<topic-id>.json
"""

import argparse
import json
import os
import sys
import time
import urllib.error
import urllib.request

BASE_URL = "https://ethereum-magicians.org"
OUTPUT_DIR = "data/magicians"

# Rate limit: Discourse default is 60 req/min for anon
REQUEST_DELAY = 1.0


def fetch_json(path):
    """Fetch JSON from Discourse API with retry."""
    url = f"{BASE_URL}{path}"
    for attempt in range(3):
        try:
            req = urllib.request.Request(url, headers={
                "Accept": "application/json",
                "User-Agent": "eth2028-research-fetcher/1.0",
            })
            with urllib.request.urlopen(req, timeout=30) as resp:
                return json.loads(resp.read().decode())
        except urllib.error.HTTPError as e:
            if e.code == 429:
                wait = 2 ** (attempt + 2)
                print(f"  Rate limited, waiting {wait}s...")
                time.sleep(wait)
                continue
            raise
        except urllib.error.URLError:
            if attempt < 2:
                time.sleep(2)
                continue
            raise
    return None


def get_categories():
    """Get all Discourse categories."""
    data = fetch_json("/categories.json")
    if not data:
        return []
    cats = []
    for cat in data.get("category_list", {}).get("categories", []):
        cats.append({
            "id": cat["id"],
            "name": cat["name"],
            "slug": cat["slug"],
            "topic_count": cat.get("topic_count", 0),
        })
        # Include subcategories
        for sub in cat.get("subcategory_list", []):
            cats.append({
                "id": sub["id"],
                "name": f"{cat['name']}/{sub['name']}",
                "slug": sub["slug"],
                "topic_count": sub.get("topic_count", 0),
                "parent_slug": cat["slug"],
            })
    return cats


def fetch_category_topics(category_slug, category_id, limit=None):
    """Fetch all topic listings for a category."""
    topics = []
    page = 0
    while True:
        data = fetch_json(f"/c/{category_slug}/{category_id}.json?page={page}")
        if not data:
            break
        topic_list = data.get("topic_list", {})
        batch = topic_list.get("topics", [])
        if not batch:
            break
        topics.extend(batch)
        if limit and len(topics) >= limit:
            topics = topics[:limit]
            break
        more_url = topic_list.get("more_topics_url")
        if not more_url:
            break
        page += 1
        time.sleep(REQUEST_DELAY)
    return topics


def fetch_topic(topic_id):
    """Fetch full topic with all posts."""
    data = fetch_json(f"/t/{topic_id}.json")
    if not data:
        return None
    time.sleep(REQUEST_DELAY)

    # If topic has more posts than the first page, fetch them
    post_stream = data.get("post_stream", {})
    stream_ids = post_stream.get("stream", [])
    loaded_ids = {p["id"] for p in post_stream.get("posts", [])}
    missing = [pid for pid in stream_ids if pid not in loaded_ids]

    # Fetch missing posts in chunks of 20
    while missing:
        chunk = missing[:20]
        missing = missing[20:]
        params = "&".join(f"post_ids[]={pid}" for pid in chunk)
        more = fetch_json(f"/t/{topic_id}/posts.json?{params}")
        if more and "post_stream" in more:
            post_stream["posts"].extend(more["post_stream"].get("posts", []))
        time.sleep(REQUEST_DELAY)

    return data


def search_topics(query, limit=50):
    """Search Discourse for topics matching a query."""
    encoded = urllib.request.quote(query)
    data = fetch_json(f"/search.json?q={encoded}")
    if not data:
        return []
    topics = data.get("topics", [])
    if limit:
        topics = topics[:limit]
    return topics


def save_topic(category_slug, topic_data, output_dir=None):
    """Save topic JSON to disk."""
    base = output_dir or OUTPUT_DIR
    cat_dir = os.path.join(base, category_slug)
    os.makedirs(cat_dir, exist_ok=True)
    topic_id = topic_data.get("id", "unknown")
    filepath = os.path.join(cat_dir, f"{topic_id}.json")
    with open(filepath, "w") as f:
        json.dump(topic_data, f, indent=2, ensure_ascii=False)
    return filepath


def main():
    parser = argparse.ArgumentParser(description="Fetch ethereum-magicians.org topics")
    parser.add_argument("--category", help="Category slug to fetch (e.g., eips, ercs)")
    parser.add_argument("--search", help="Search query")
    parser.add_argument("--limit", type=int, default=None, help="Max topics per category")
    parser.add_argument("--list-categories", action="store_true", help="List categories and exit")
    parser.add_argument("--topics-only", action="store_true", help="Fetch topic listings only (no full content)")
    parser.add_argument("--output", default=OUTPUT_DIR, help=f"Output directory (default: {OUTPUT_DIR})")
    args = parser.parse_args()

    out = args.output

    if args.list_categories:
        cats = get_categories()
        for cat in cats:
            print(f"  {cat['slug']:30s}  ({cat['topic_count']:5d} topics)  {cat['name']}")
        return

    if args.search:
        print(f"Searching for: {args.search}")
        topics = search_topics(args.search, limit=args.limit or 50)
        print(f"  Found {len(topics)} topics")
        os.makedirs(os.path.join(out, "search"), exist_ok=True)
        for t in topics:
            if args.topics_only:
                save_topic("search", t, out)
            else:
                full = fetch_topic(t["id"])
                if full:
                    save_topic("search", full, out)
                    title = full.get("title", "?")
                    print(f"    [{t['id']}] {title}")
        return

    # Fetch by category
    cats = get_categories()
    if args.category:
        cats = [c for c in cats if c["slug"] == args.category]
        if not cats:
            print(f"Category '{args.category}' not found. Use --list-categories to see available.")
            sys.exit(1)

    for cat in cats:
        slug = cat["slug"]
        print(f"\n=== {cat['name']} ({cat['topic_count']} topics) ===")
        topics = fetch_category_topics(slug, cat["id"], limit=args.limit)
        print(f"  Fetched {len(topics)} topic listings")

        if args.topics_only:
            # Save just the listing
            os.makedirs(os.path.join(out, slug), exist_ok=True)
            listing_path = os.path.join(out, slug, "_listing.json")
            with open(listing_path, "w") as f:
                json.dump(topics, f, indent=2, ensure_ascii=False)
            print(f"  Saved listing to {listing_path}")
            continue

        for i, t in enumerate(topics):
            topic_id = t["id"]
            full = fetch_topic(topic_id)
            if full:
                path = save_topic(slug, full, out)
                title = full.get("title", "?")
                print(f"  [{i+1}/{len(topics)}] {title}")


if __name__ == "__main__":
    main()
