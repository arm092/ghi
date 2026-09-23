package am.ghi.ide;
import com.intellij.openapi.options.Configurable;
import com.intellij.openapi.project.Project;
import com.intellij.ui.components.JBTextField;
import javax.swing.*;
import java.awt.*;
import java.util.Objects;
public final class GhiConfigurable implements Configurable {
    private final Project project;
    private JBTextField executable,directory,arguments;
    public GhiConfigurable(Project project){this.project=project;}
    public String getDisplayName(){return "Ghi";}
    public JComponent createComponent(){
        executable=new JBTextField();directory=new JBTextField();arguments=new JBTextField();
        JPanel fields=new JPanel(new GridLayout(0,1,0,6));
        fields.add(new JLabel("Compiler executable (blank: installed Ghi or PATH)"));fields.add(executable);
        fields.add(new JLabel("Project directory (blank: opened project; relative paths allowed)"));fields.add(directory);
        fields.add(new JLabel("Program arguments (Run only; quote arguments containing spaces)"));fields.add(arguments);
        JPanel panel=new JPanel(new BorderLayout());panel.add(fields,BorderLayout.NORTH);reset();return panel;
    }
    public boolean isModified(){var state=GhiSettings.get(project).getState();return executable!=null &&
        (!Objects.equals(executable.getText(),state.executable)||!Objects.equals(directory.getText(),state.directory)||!Objects.equals(arguments.getText(),state.arguments));}
    public void apply(){var state=GhiSettings.get(project).getState();state.executable=executable.getText().trim();state.directory=directory.getText().trim();state.arguments=arguments.getText();}
    public void reset(){if(executable==null)return;var state=GhiSettings.get(project).getState();executable.setText(state.executable);directory.setText(state.directory);arguments.setText(state.arguments);}
    public void disposeUIResources(){executable=null;directory=null;arguments=null;}
}
