import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { verb } from "@/api/request";
import { HostIdentities, TeaCard } from "./accounts";

vi.mock(import("@/api/request"), async (importOriginal) => ({
  ...(await importOriginal()),
  verb: vi.fn(async () => ({ name: "codeberg.org" })) as never,
}));
vi.mock("@/lib/notify", () => ({ notify: vi.fn() }));

const login = (name: string, isDefault: boolean) => ({
  name,
  url: `https://${name}`,
  user: "octo",
  default: isDefault,
});

describe("TeaCard", () => {
  beforeEach(() => vi.mocked(verb).mockClear());

  it("signs in with a server and a token, then clears the token", async () => {
    render(<TeaCard forge={undefined} onSignOut={vi.fn()} />);
    const server = screen.getByLabelText("Server");
    expect((server as HTMLInputElement).value).toBe("https://gitea.com");
    const signIn = screen.getByRole("button", { name: "Sign in" });
    expect((signIn as HTMLButtonElement).disabled).toBe(true);
    fireEvent.change(server, { target: { value: "https://codeberg.org/" } });
    expect(
      screen.getByRole("link", { name: /Create a token on codeberg.org/ }).getAttribute("href"),
    ).toBe("https://codeberg.org/user/settings/applications");
    const token = screen.getByLabelText("Access token");
    fireEvent.change(token, { target: { value: "t0k3n" } });
    fireEvent.click(signIn);
    await vi.waitFor(() => expect((token as HTMLInputElement).value).toBe(""));
    expect(verb).toHaveBeenCalledWith("forge.token", {
      forge: "tea",
      url: "https://codeberg.org/",
      token: "t0k3n",
    });
  });

  it("refuses an http server", () => {
    render(<TeaCard forge={undefined} onSignOut={vi.fn()} />);
    fireEvent.change(screen.getByLabelText("Server"), { target: { value: "http://gitea.com" } });
    fireEvent.change(screen.getByLabelText("Access token"), { target: { value: "t" } });
    expect((screen.getByRole("button", { name: "Sign in" }) as HTMLButtonElement).disabled).toBe(
      true,
    );
    expect(screen.queryByRole("link")).toBeNull();
  });

  it("lists each server with its own sign-out", () => {
    const onSignOut = vi.fn();
    const logins = [login("git.example.org", false), login("codeberg.org", true)];
    render(
      <TeaCard
        forge={{ host: "codeberg.org", signed_in: true, user: "octo", logins }}
        onSignOut={onSignOut}
      />,
    );
    expect(screen.getByText("default")).toBeTruthy();
    expect(screen.getByRole("heading", { name: "Add a server" })).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Sign out of git.example.org" }));
    expect(onSignOut).toHaveBeenCalledWith(logins[0]);
  });
});

describe("HostIdentities", () => {
  beforeEach(() => vi.mocked(verb).mockClear());

  it("adds an identity for a signed-in host", async () => {
    render(
      <HostIdentities identities={[]} suggested={["gitlab.com"]} defaultName="Ada" ask={vi.fn()} />,
    );
    fireEvent.click(screen.getByRole("button", { name: "Add a host identity" }));
    expect((screen.getByLabelText("Host") as HTMLInputElement).value).toBe("gitlab.com");
    expect((screen.getByLabelText("Name") as HTMLInputElement).value).toBe("Ada");
    const save = screen.getByRole("button", { name: "Save" }) as HTMLButtonElement;
    expect(save.disabled).toBe(true);
    fireEvent.change(screen.getByLabelText("Email"), { target: { value: "ada@example.org" } });
    fireEvent.click(save);
    await vi.waitFor(() =>
      expect(verb).toHaveBeenCalledWith("git.identity", {
        host: "gitlab.com",
        name: "Ada",
        email: "ada@example.org",
      }),
    );
  });

  it("refuses a host that already has one", () => {
    const identities = [{ host: "gitlab.com", name: "Ada", email: "ada@example.org" }];
    render(<HostIdentities identities={identities} suggested={[]} defaultName="" ask={vi.fn()} />);
    expect(screen.getByText("Ada <ada@example.org>")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Add a host identity" }));
    fireEvent.change(screen.getByLabelText("Host"), { target: { value: "https://GitLab.com/" } });
    expect(screen.getByText(/already has an identity/)).toBeTruthy();
  });

  it("confirms before removing one", () => {
    const ask = vi.fn();
    const id = { host: "github.com", name: "Ada", email: "ada@example.org" };
    render(<HostIdentities identities={[id]} suggested={[]} defaultName="" ask={ask} />);
    fireEvent.click(screen.getByRole("button", { name: "Remove the identity for github.com" }));
    expect(ask).toHaveBeenCalledWith(expect.objectContaining({ confirm: "Remove", danger: true }));
    expect(verb).not.toHaveBeenCalled();
  });
});
